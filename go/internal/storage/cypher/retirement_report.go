// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RetirementReport is the attributed path-conflict retirement statement's
// RETURN payload: the pre-delete incident-edge collection plus the retired
// Repository ids, one per RETURN row. The zero value is unattributed: the
// executor reported no RETURN rows (a backend that cannot return records,
// or a statement that is not the retirement).
type RetirementReport struct {
	// Dropped carries the first RETURN row's incident-edge collection.
	// Non-nil, even when empty, means the executor consumed the RETURN
	// record: an empty slice is an attributed edgeless retirement. Only
	// the first row's collection feeds the dropped-edge metrics; later
	// rows of a multi-node retirement are counted in the summary
	// relationships_deleted but not in the per-type split (#7324 item).
	Dropped []DroppedEdge
	// RetiredIDs carries every RETURN row's retired Repository id. Unlike
	// Dropped it drains all rows: a path holding two stale repositories
	// must invalidate both fingerprint sets (#8316).
	RetiredIDs []string
}

// ReportRetirementWriteCounts appends one entry carrying the retirement
// statement's RETURN payload. Bolt seams that consumed the statement's
// RETURN record pass what they collected; every other call passes the zero
// report, leaving the entry unattributed.
func ReportRetirementWriteCounts(ctx context.Context, cypher string, params map[string]any, counters WriteCounters, report RetirementReport) {
	WriteCountsCollectorFromContext(ctx).Add(WriteCountEntry{Cypher: cypher, Parameters: params, Counters: counters, Dropped: report.Dropped, RetiredIDs: report.RetiredIDs})
}

// IsRepositoryPathCleanupCypher reports whether cypher is the ATTRIBUTED
// path-conflict retirement statement. Bolt seams use it to decide whether a
// RETURN record is waiting to be consumed; identity is the constant
// itself, the same equality the Rule 1 guard pins. The bare retirement
// shape (canonicalNodeRepositoryPathCleanupBareCypher, what NornicDB writers
// emit) carries no RETURN record, so it is deliberately not matched here:
// CollectRetirementReport leaves its result untouched and its entry
// stays unattributed.
func IsRepositoryPathCleanupCypher(cypher string) bool {
	return cypher == canonicalNodeRepositoryPathCleanupCypher
}

// CollectRetirementReport consumes the retirement statement's RETURN records
// ahead of the summary Consume, returning the entry payload for
// ReportRetirementWriteCounts. It returns the zero report for every other
// statement without touching the result, so uncaptured and non-retirement
// runs pay one string comparison per statement. A retirement whose MATCH
// matched nothing yields zero records (steady state) and also returns the
// zero report; a matched edgeless retirement yields one record holding an
// empty dropped list, which parses to a non-nil empty slice (attributed,
// empty split). A record error here needs no separate return: the caller's
// Consume re-surfaces it and fails the statement the same way.
func CollectRetirementReport(ctx context.Context, cypher string, result neo4jdriver.ResultWithContext) RetirementReport {
	var report RetirementReport
	if !IsRepositoryPathCleanupCypher(cypher) {
		return report
	}
	if result == nil {
		return report
	}
	for result.Next(ctx) {
		record := result.Record()
		if record == nil {
			continue
		}
		if value, found := record.Get("retired_id"); found {
			if retiredID, ok := value.(string); ok && retiredID != "" {
				report.RetiredIDs = append(report.RetiredIDs, retiredID)
			}
		}
		if report.Dropped != nil {
			continue
		}
		value, found := record.Get("dropped")
		if !found {
			continue
		}
		report.Dropped = ParseDroppedEdges(value)
	}
	return report
}

// CollectRetiredRepositoryIDs drains the retirement statement's RETURN
// records for retired Repository ids (#8316), one per row. It is the
// id-only view of CollectRetirementReport for callers that never touch the
// dropped-edge metrics: same no-touch rule for other statements, same zero
// rows in steady state.
func CollectRetiredRepositoryIDs(ctx context.Context, cypher string, result neo4jdriver.ResultWithContext) []string {
	return CollectRetirementReport(ctx, cypher, result).RetiredIDs
}

// RetiredRepositoryObserver runs after a committed path-conflict retirement
// that deleted a Repository node, with the union of retired Repository ids
// across the call's reported attempts (#8316). The graph delete already
// committed, so an error is logged and never fails the write: the
// invalidation is best-effort, and the gap reconciliation re-derives any
// loss a failed observer call misses. Nil (the default) observes nothing.
type RetiredRepositoryObserver func(ctx context.Context, repoIDs []string) error

// unionRetiredRepositoryIDs returns the deduplicated retired Repository ids
// across every collected retirement entry, in first-seen order. Entries
// accumulate across driver retries, and a retry that matched nothing after
// an earlier attempt committed still leaves that attempt's ids behind, so
// the union — not the last entry — is the repair set.
func unionRetiredRepositoryIDs(entries []WriteCountEntry) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, entry := range entries {
		for _, id := range entry.RetiredIDs {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// ParseDroppedEdges converts one RETURN dropped driver value to the entry
// payload: a list of {t, dir, src} maps. Malformed rows (not a map, or
// missing the type or direction) are skipped: the statement only ever
// collects well-formed maps, so anything else is driver-shape drift, not a
// dropped edge. A missing src becomes "": the writer classes it as other.
func ParseDroppedEdges(value any) []DroppedEdge {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]DroppedEdge, 0, len(rows))
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			continue
		}
		direction, _ := fields["dir"].(string)
		edgeType, _ := fields["t"].(string)
		if direction == "" || edgeType == "" {
			continue
		}
		evidenceSource, _ := fields["src"].(string)
		out = append(out, DroppedEdge{Direction: direction, Type: edgeType, EvidenceSource: evidenceSource})
	}
	return out
}

// --- Retirement attribution report ---
//
// reportRepositoryRetirement and summarizeDroppedEdges consume the entry
// payload: the writer calls the report after the retirement commits,
// the observer (when set) receives the union of retired Repository ids so
// fingerprint invalidation can follow the graph commit (#8316), and the
// summary folds one attributed collection into the direction
// totals, the closed counter buckets, and the capped per-type log map.

// reportRepositoryRetirement publishes what the committed path-conflict
// retirement deleted (#7324). The statement DETACH DELETEs a different-id
// Repository still holding this path, which drops every relationship on it:
// reducer edges other scopes wrote into it, which nothing re-arms, and its
// own projector edges. relationships_deleted is the backend's count of both
// directions; the per-direction/per-type split comes from the statement's
// own pre-delete RETURN dropped collection (#7324 item).
//
// The last collected entry is the committed attempt: a driver or executor
// retry re-runs the statement and reports again, and Write calls this only
// after the statement's transaction committed. The `canonical repository
// retired` line and the counter appear only when the statement deleted a
// node; a retirement that matched nothing (the steady state) records nothing
// at INFO or WARN. With no entry at all the executor reported no write
// summary, so nothing is known about the retirement: a DEBUG line with a
// distinct message and deletes_counted=false says so, and no counter moves.
// A retirement that committed but whose driver call still returned an error
// is re-run on retry, matches nothing, and so is not counted: the count is
// at most once per retirement, not exactly once.
//
// Attribution is closed over: entry.Dropped == nil means the executor never
// consumed the RETURN dropped record (a backend that cannot return records,
// such as the NornicDB phase-group path, which stays unattributed), so the
// log carries attribution=false and only
// eshu_dp_canonical_repository_retirements_total moves. A non-nil slice,
// even empty, is an attributed retirement: the log carries attribution=true
// with dropped_incoming/dropped_outgoing and the capped per-type map, and
// eshu_dp_canonical_repository_retired_relationships_total moves per
// closed (direction, class) bucket.
func (w *CanonicalNodeWriter) reportRepositoryRetirement(
	ctx context.Context,
	mat canonical.CanonicalMaterialization,
	collector *WriteCountsCollector,
) {
	if collector == nil || mat.Repository == nil {
		return
	}
	entries := collector.Entries()
	fields := []any{
		"scope_id", mat.ScopeID,
		"generation_id", mat.GenerationID,
		"repo_id", mat.Repository.RepoID,
		"path", mat.Repository.Path,
	}
	if len(entries) == 0 {
		slog.DebugContext(ctx, "canonical repository retirement not counted: executor reported no write summary",
			append(fields, "deletes_counted", false, "attribution", false)...)
		return
	}
	if w.retiredRepositoryObserver != nil {
		if retired := unionRetiredRepositoryIDs(entries); len(retired) > 0 {
			if err := w.retiredRepositoryObserver(ctx, retired); err != nil {
				slog.WarnContext(ctx, "canonical repository retirement observer failed",
					append(fields, "retired_ids", retired, "error", err)...)
			}
		}
	}
	last := entries[len(entries)-1]
	counters := last.Counters
	if counters.NodesDeleted == 0 {
		return
	}
	outcome, level := telemetry.RepositoryRetirementOutcomeClean, slog.LevelInfo
	if counters.RelationshipsDeleted > 0 {
		outcome, level = telemetry.RepositoryRetirementOutcomeDroppedRelationships, slog.LevelWarn
	}
	if w.instruments != nil && w.instruments.CanonicalRepositoryRetirements != nil {
		w.instruments.CanonicalRepositoryRetirements.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	}
	fields = append(fields,
		"nodes_deleted", counters.NodesDeleted,
		"relationships_deleted", counters.RelationshipsDeleted,
		"deletes_counted", true,
		"outcome", outcome,
	)
	if last.Dropped == nil {
		slog.Log(ctx, level, "canonical repository retired", append(fields, "attribution", false)...)
		return
	}
	split := summarizeDroppedEdges(last.Dropped)
	if w.instruments != nil && w.instruments.CanonicalRepositoryRetiredRelationships != nil {
		for bucket, count := range split.buckets {
			w.instruments.CanonicalRepositoryRetiredRelationships.Add(ctx, count, metric.WithAttributes(
				telemetry.AttrDirection(bucket.direction),
				telemetry.AttrEdgeClass(bucket.class),
			))
		}
	}
	slog.Log(ctx, level, "canonical repository retired", append(fields,
		"attribution", true,
		"dropped_incoming", split.incoming,
		"dropped_outgoing", split.outgoing,
		"dropped_by_type", split.byType,
	)...)
}

// retiredRelationshipBucket is one closed (direction, class) cell of the
// retired-relationship counter: 2 directions x 3 classes, 6 series max.
type retiredRelationshipBucket struct {
	direction string
	class     string
}

// droppedEdgeSplit is the attributed retirement's per-direction/per-type
// summary. buckets holds the exact per-cell counts for the counter;
// incoming/outgoing are the direction totals (their sum is the attributed
// edge count); byType is the capped per-type log map.
type droppedEdgeSplit struct {
	buckets  map[retiredRelationshipBucket]int64
	incoming int64
	outgoing int64
	byType   map[string]int64
}

// droppedEdgeTypeLogCap bounds the per-type log map: the top
// droppedEdgeTypeLogCap types by count are named, and overflow_n carries how
// many further distinct types were omitted. Raw types never become metric
// labels, so the log is the only place they appear, and the cap keeps one
// pathological retirement from flooding it.
const droppedEdgeTypeLogCap = 10

// summarizeDroppedEdges folds one attributed retirement's dropped edges into
// the counter buckets, the direction totals, and the capped per-type log
// map. Direction follows the stored edge ("out" only when the retired node
// starts it, everything else incoming, so the direction total stays exact);
// class follows the evidence_source (projector/* is projector, any other
// present source is reducer, missing is other).
func summarizeDroppedEdges(dropped []DroppedEdge) droppedEdgeSplit {
	split := droppedEdgeSplit{buckets: map[retiredRelationshipBucket]int64{}}
	byTypeCounts := map[string]int64{}
	for _, edge := range dropped {
		direction := telemetry.RetiredRelationshipDirectionIncoming
		if edge.Direction == DroppedEdgeOutgoing {
			direction = telemetry.RetiredRelationshipDirectionOutgoing
		}
		var class string
		switch {
		case edge.EvidenceSource == "":
			class = telemetry.RetiredRelationshipClassOther
		case strings.HasPrefix(edge.EvidenceSource, "projector/"):
			class = telemetry.RetiredRelationshipClassProjector
		default:
			class = telemetry.RetiredRelationshipClassReducer
		}
		split.buckets[retiredRelationshipBucket{direction: direction, class: class}]++
		if direction == telemetry.RetiredRelationshipDirectionOutgoing {
			split.outgoing++
		} else {
			split.incoming++
		}
		byTypeCounts[direction+":"+edge.Type]++
	}
	type typeCount struct {
		name  string
		count int64
	}
	ranked := make([]typeCount, 0, len(byTypeCounts))
	for name, count := range byTypeCounts {
		ranked = append(ranked, typeCount{name: name, count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].name < ranked[j].name
	})
	split.byType = make(map[string]int64, min(len(ranked), droppedEdgeTypeLogCap)+1)
	for i, entry := range ranked {
		if i >= droppedEdgeTypeLogCap {
			break
		}
		split.byType[entry.name] = entry.count
	}
	split.byType["overflow_n"] = int64(max(len(ranked)-droppedEdgeTypeLogCap, 0))
	return split
}
