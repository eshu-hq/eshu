// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WriteCounters carries one executed write statement's Bolt summary
// counters: graph objects the statement created, deleted, or relabeled.
// Unlike RecordReconciliationDriftRetractions (reconciliation_drift_metrics.go),
// which publishes retract deletes to OTEL, these counters travel to the
// differential capture recorder (issue #6783), not to telemetry. Backends
// that do not report a counter leave it zero.
type WriteCounters struct {
	NodesCreated         int64
	NodesDeleted         int64
	RelationshipsCreated int64
	RelationshipsDeleted int64
	PropertiesSet        int64
	LabelsAdded          int64
	LabelsRemoved        int64
}

// Add returns the counter-wise sum: retry and drain-loop re-executions of
// one statement report every attempt, so the recorder join sums rather than
// dropping surplus entries.
func (c WriteCounters) Add(other WriteCounters) WriteCounters {
	return WriteCounters{
		NodesCreated:         c.NodesCreated + other.NodesCreated,
		NodesDeleted:         c.NodesDeleted + other.NodesDeleted,
		RelationshipsCreated: c.RelationshipsCreated + other.RelationshipsCreated,
		RelationshipsDeleted: c.RelationshipsDeleted + other.RelationshipsDeleted,
		PropertiesSet:        c.PropertiesSet + other.PropertiesSet,
		LabelsAdded:          c.LabelsAdded + other.LabelsAdded,
		LabelsRemoved:        c.LabelsRemoved + other.LabelsRemoved,
	}
}

// DroppedEdge is one relationship the path-conflict retirement statement
// collected before it deleted the retired Repository (#7324 item:
// per-direction/per-type split). Direction is the stored edge's direction
// from the retired node: DroppedEdgeOutgoing when the retired node starts
// the edge, DroppedEdgeIncoming otherwise. Type is the raw relationship
// type, kept for log attribution only and never used as a metric label.
// EvidenceSource is the edge's evidence_source property, "" when missing.
type DroppedEdge struct {
	Direction      string
	Type           string
	EvidenceSource string
}

// Closed DroppedEdge direction values, matching the CASE the retirement
// statement collects. The writer maps them to the incoming/outgoing metric
// labels; anything else a future statement collects falls into incoming so
// the direction total stays exact.
const (
	// DroppedEdgeIncoming means the retired node is the edge's end node.
	DroppedEdgeIncoming = "in"
	// DroppedEdgeOutgoing means the retired node is the edge's start node.
	DroppedEdgeOutgoing = "out"
)

// WriteCountEntry is one Bolt-reported write execution: the statement text
// and bound parameters that identify it plus its summary counters.
type WriteCountEntry struct {
	Cypher     string
	Parameters map[string]any
	Counters   WriteCounters
	// Dropped carries the retirement statement's pre-delete incident-edge
	// collection. Nil means unattributed: the executor reported no RETURN
	// dropped rows (a backend that cannot return records, or a statement
	// that is not the retirement). Non-nil, even when empty, means the
	// executor consumed the RETURN dropped record: an empty slice is an
	// attributed edgeless retirement.
	Dropped []DroppedEdge
}

// WriteCountsCollector gathers write-count entries in arrival order. It is
// safe for concurrent use: entity-phase concurrent chunks share one call's
// collector through the context. Entries snapshots copy, so joining never
// observes a torn list.
type WriteCountsCollector struct {
	mutex   sync.Mutex
	entries []WriteCountEntry
	// parent receives every entry this collector is offered, so a narrower
	// collector stashed below an outer one never hides entries from it.
	parent *WriteCountsCollector
	// accept, when set, limits which entries this collector keeps.
	accept func(WriteCountEntry) bool
}

// NewWriteCountsCollector returns an empty collector.
func NewWriteCountsCollector() *WriteCountsCollector {
	return &WriteCountsCollector{}
}

// NewFilteredWriteCountsCollector returns an empty collector that keeps only
// the entries accept admits and forwards every entry, kept or not, to parent
// (nil for none). A writer that needs one statement's counters stashes it
// over whatever collector the caller already stashed (the differential
// capture recorder, issue #6783), so that recorder still sees every
// statement while the writer holds only the entries it reads (#7324).
func NewFilteredWriteCountsCollector(parent *WriteCountsCollector, accept func(WriteCountEntry) bool) *WriteCountsCollector {
	return &WriteCountsCollector{parent: parent, accept: accept}
}

// Add forwards the entry to the parent collector, then appends it when this
// collector's filter admits it.
func (c *WriteCountsCollector) Add(entry WriteCountEntry) {
	if c == nil {
		return
	}
	c.parent.Add(entry)
	if c.accept != nil && !c.accept(entry) {
		return
	}
	c.mutex.Lock()
	c.entries = append(c.entries, entry)
	c.mutex.Unlock()
}

// Entries returns a copy of the entries in arrival order.
func (c *WriteCountsCollector) Entries() []WriteCountEntry {
	if c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	out := make([]WriteCountEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// writeCountsContextKey carries one recorder call's collector. An unexported
// struct key keeps unrelated context values from colliding with it.
type writeCountsContextKey struct{}

// WithWriteCountsCollector stashes the collector for one recorder call in
// the context. Bolt seams below read it back; every other layer passes the
// context through untouched.
func WithWriteCountsCollector(ctx context.Context, collector *WriteCountsCollector) context.Context {
	return context.WithValue(ctx, writeCountsContextKey{}, collector)
}

// WriteCountsCollectorFromContext returns the stashed collector, or nil
// when capture is off or the call predates it.
func WriteCountsCollectorFromContext(ctx context.Context) *WriteCountsCollector {
	collector, _ := ctx.Value(writeCountsContextKey{}).(*WriteCountsCollector)
	return collector
}

// ReportWriteCounts appends one entry to the call's collector. It is a
// no-op without a stashed collector, so production Bolt seams call it
// unconditionally: uncaptured runs pay one context lookup per statement.
func ReportWriteCounts(ctx context.Context, cypher string, params map[string]any, counters WriteCounters) {
	ReportWriteCountsWithDropped(ctx, cypher, params, counters, nil)
}

// ReportWriteCountsWithDropped appends one entry carrying the retirement
// statement's pre-delete incident-edge collection. Bolt seams that consumed
// the statement's RETURN dropped record pass what they collected (non-nil,
// even when empty); every other call passes nil, leaving the entry
// unattributed.
func ReportWriteCountsWithDropped(ctx context.Context, cypher string, params map[string]any, counters WriteCounters, dropped []DroppedEdge) {
	WriteCountsCollectorFromContext(ctx).Add(WriteCountEntry{Cypher: cypher, Parameters: params, Counters: counters, Dropped: dropped})
}

// IsRepositoryPathCleanupCypher reports whether cypher is the ATTRIBUTED
// path-conflict retirement statement. Bolt seams use it to decide whether a
// RETURN dropped record is waiting to be consumed; identity is the constant
// itself, the same equality the Rule 1 guard pins. The bare retirement
// shape (canonicalNodeRepositoryPathCleanupBareCypher, what NornicDB writers
// emit) carries no RETURN record, so it is deliberately not matched here:
// CollectRetiredRelationships leaves its result untouched and its entry
// stays unattributed.
func IsRepositoryPathCleanupCypher(cypher string) bool {
	return cypher == canonicalNodeRepositoryPathCleanupCypher
}

// CollectRetiredRelationships consumes the retirement statement's single
// RETURN dropped record ahead of the summary Consume, returning the entry
// payload for ReportWriteCountsWithDropped. It returns nil for every other
// statement without touching the result, so uncaptured and non-retirement
// runs pay one string comparison per statement. A retirement whose MATCH
// matched nothing yields zero records (steady state) and also returns nil;
// a matched edgeless retirement yields one record holding an empty list,
// which parses to a non-nil empty slice (attributed, empty split). A record
// error here needs no separate return: the caller's Consume re-surfaces it
// and fails the statement the same way.
func CollectRetiredRelationships(ctx context.Context, cypher string, result neo4jdriver.ResultWithContext) []DroppedEdge {
	if !IsRepositoryPathCleanupCypher(cypher) {
		return nil
	}
	if result == nil || !result.Next(ctx) {
		return nil
	}
	record := result.Record()
	if record == nil {
		return nil
	}
	value, found := record.Get("dropped")
	if !found {
		return nil
	}
	return ParseDroppedEdges(value)
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

// WriteSummaryCounters is the Bolt summary-counter surface
// WriteCountersFromSummary reads. It matches the driver's Counters shape
// for the graph-object counters, so seams pass summary.Counters() directly;
// the test fake implements it without a driver.
type WriteSummaryCounters interface {
	NodesCreated() int
	NodesDeleted() int
	RelationshipsCreated() int
	RelationshipsDeleted() int
	PropertiesSet() int
	LabelsAdded() int
	LabelsRemoved() int
}

// WriteCountersFromSummary converts one Bolt summary's graph-object
// counters to WriteCounters. Schema counters (indexes, constraints) stay
// out: production writers never change schema, so they carry no statement
// truth.
func WriteCountersFromSummary(counters WriteSummaryCounters) WriteCounters {
	return WriteCounters{
		NodesCreated:         int64(counters.NodesCreated()),
		NodesDeleted:         int64(counters.NodesDeleted()),
		RelationshipsCreated: int64(counters.RelationshipsCreated()),
		RelationshipsDeleted: int64(counters.RelationshipsDeleted()),
		PropertiesSet:        int64(counters.PropertiesSet()),
		LabelsAdded:          int64(counters.LabelsAdded()),
		LabelsRemoved:        int64(counters.LabelsRemoved()),
	}
}

// --- Retirement attribution report ---
//
// reportRepositoryRetirement and summarizeDroppedEdges consume the entry
// payload above: the writer calls the report after the retirement commits,
// and the summary folds one attributed collection into the direction
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
