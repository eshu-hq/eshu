// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ProjectedSourceEdgeRow is one enumerated CloudResource source edge from the
// graph, carrying the identity fields the projected-source ledger needs. It
// covers all four evidence-source families the ledger is shared across (AWS,
// Azure, GCP relationship edges, and observability coverage edges) since the
// enumeration query is a single bare-relationship scan filtered by
// evidence_source rather than one query per edge label.
type ProjectedSourceEdgeRow struct {
	EvidenceSource string
	ScopeID        string
	GenerationID   string
	SourceUID      string
}

// projectedSourceEdgeBackfillQuerier is the read surface the backfill
// orchestrator needs. Unlike codeInterprocBackfillQuerier and
// codeTaintNodeBackfillQuerier, this interface has no fast count-guard method:
// AWS/Azure/GCP relationship types are an open vocabulary (new relationship
// types can be added by writers without a schema change), so there is no
// bare-type or bare-label index count that stays a fast lookup. A bare
// bare-relationship count (`MATCH ()-[r]->() RETURN count(r)`) would force a
// whole-graph relationship scan — the exact full scan the count guard exists
// to avoid — so it is deliberately omitted here. Production code satisfies
// this via ProjectedSourceEdgeBackfillReader (backed by GraphQueryRunner);
// tests supply a fake.
type projectedSourceEdgeBackfillQuerier interface {
	EnumerateProjectedSourceEdges(ctx context.Context, evidenceSources []string) ([]ProjectedSourceEdgeRow, error)
}

// ProjectedSourceEdgeBackfiller seeds the ProjectedSourceLedger from existing
// graph CloudResource source edges (AWS/Azure/GCP relationship edges and
// observability coverage edges) so the ledger is a superset of graph edges at
// deploy time. Edges projected BEFORE this deploy have no ledger rows; without
// this backfill the first post-deploy ledger-anchored retract
// (RetractXxxByUIDs) would enumerate an empty ledger for those edges and
// orphan them in the graph instead of retracting them.
//
// This mirrors CodeInterprocProjectedEdgeBackfiller and
// CodeTaintEvidenceProjectedNodeBackfiller with one structural difference: it
// has no bare-type/bare-label count guard (see projectedSourceEdgeBackfillQuerier),
// so the StateMarker is not an optional optimization here — it is the only
// idempotency guard against a full-graph enumeration running on every
// reducer startup. A nil StateMarker makes Run a no-op rather than falling
// back to an unguarded full scan.
type ProjectedSourceEdgeBackfiller struct {
	// Reader is the backfill graph-read surface (EnumerateProjectedSourceEdges).
	// Production wiring passes a ProjectedSourceEdgeBackfillReader backed by
	// GraphQueryRunner.
	Reader projectedSourceEdgeBackfillQuerier

	// Ledger is the durable projected-source-edge store. When nil, Run is a
	// no-op.
	Ledger ProjectedSourceLedger

	// StateMarker is the durable per-source completion-marker store. When
	// nil, Run is a no-op (see type doc: there is no ledger-based fallback
	// completion check available for this open-vocabulary edge family).
	StateMarker CodeValueFlowBackfillStateMarker

	// EvidenceSources is the set of evidence_source values to backfill: the
	// four CloudResource edge families (AWS relationship, Azure relationship,
	// GCP relationship, observability coverage) that share the
	// ProjectedSourceLedger.
	EvidenceSources []string

	// Now returns the current time (injected for test determinism).
	Now func() time.Time
}

// ProjectedSourceEdgeBackfillReader provides the graph-read capability the
// backfill orchestrator needs, backed by GraphQueryRunner. It is the
// production reader; tests supply a fake implementing
// projectedSourceEdgeBackfillQuerier.
type ProjectedSourceEdgeBackfillReader struct {
	Graph GraphQueryRunner
}

// EnumerateProjectedSourceEdges runs the one-time full scan of CloudResource
// source edges for the given evidence sources. AWS, Azure, and GCP
// relationship types are an open vocabulary (new relationship types can be
// introduced by writers without a graph schema change), so the query uses a
// bare relationship pattern `-[r]->()` with no type list — a type-listed
// MATCH would silently miss any relationship type added after this file was
// written. Observability coverage edges (COVERS) are also CloudResource
// source edges, so this single bare-relationship query covers all four
// evidence-source families by filtering on r.evidence_source alone. This is a
// one-time startup scan (see Run), not a hot-path query.
func (r ProjectedSourceEdgeBackfillReader) EnumerateProjectedSourceEdges(
	ctx context.Context, evidenceSources []string,
) ([]ProjectedSourceEdgeRow, error) {
	if r.Graph == nil {
		return nil, fmt.Errorf("backfill reader requires graph query runner")
	}
	rows, err := r.Graph.Run(
		ctx,
		`MATCH (s:CloudResource)-[r]->()
WHERE r.evidence_source IN $evidence_sources
RETURN s.uid AS source_uid,
       r.scope_id AS scope_id,
       r.generation_id AS generation_id,
       r.evidence_source AS evidence_source`,
		map[string]any{"evidence_sources": evidenceSources},
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate projected source edges: %w", err)
	}
	var out []ProjectedSourceEdgeRow
	for _, row := range rows {
		sourceUID := anyToString(row["source_uid"])
		scopeID := anyToString(row["scope_id"])
		genID := anyToString(row["generation_id"])
		evSrc := anyToString(row["evidence_source"])
		if sourceUID == "" || scopeID == "" || genID == "" || evSrc == "" {
			continue
		}
		out = append(out, ProjectedSourceEdgeRow{
			EvidenceSource: evSrc,
			ScopeID:        scopeID,
			GenerationID:   genID,
			SourceUID:      sourceUID,
		})
	}
	return out, nil
}

// projectedSourceEdgeBackfillKey returns the stable backfill-state marker key
// for the projected-source-edge ledger backfiller. It is namespaced
// separately from codeInterprocBackfillKey and codeTaintNodeBackfillKey so
// the three backfillers never collide on the shared
// CodeValueFlowBackfillStateStore table even though all three reuse the same
// generic marker type.
func projectedSourceEdgeBackfillKey(evidenceSource string) string {
	return "projected_source_edge:" + evidenceSource
}

// Run seeds the ledger from existing graph CloudResource source edges. It is
// a one-time, idempotent startup backfill:
//  1. Nil guard: a nil Reader, Ledger, or StateMarker makes Run a no-op.
//  2. Per-source check: a source already marked complete is excluded from
//     enumeration.
//  3. Remaining sources trigger one enumeration call, grouped by
//     (evidence_source, scope_id, generation_id) and recorded into the
//     ledger.
//  4. Every enumerated source (including one with zero matching edges) is
//     marked complete so the next startup does not re-scan the graph.
//
// There is no bare-type count guard step here (see type doc); the per-source
// completion marker is the sole guard against repeated full scans.
func (b ProjectedSourceEdgeBackfiller) Run(ctx context.Context) error {
	if b.Reader == nil || b.Ledger == nil || b.StateMarker == nil {
		return nil
	}

	var toBackfill []string
	for _, src := range b.EvidenceSources {
		done, err := b.StateMarker.IsComplete(ctx, projectedSourceEdgeBackfillKey(src))
		if err != nil {
			return fmt.Errorf("projected source edge backfill check completion for source %q: %w", src, err)
		}
		if !done {
			toBackfill = append(toBackfill, src)
		}
	}
	if len(toBackfill) == 0 {
		return nil
	}

	rows, err := b.Reader.EnumerateProjectedSourceEdges(ctx, toBackfill)
	if err != nil {
		return fmt.Errorf("projected source edge backfill enumerate: %w", err)
	}

	// Group by (evidence_source, scope_id, generation_id) and record. Rows
	// with any empty identity field are dropped defensively even though the
	// production reader already filters them, so a caller-supplied reader
	// cannot smuggle a blank-keyed row into the ledger.
	type groupKey struct {
		evidenceSource string
		scopeID        string
		generationID   string
	}
	groups := make(map[groupKey][]string)
	for _, row := range rows {
		if row.EvidenceSource == "" || row.ScopeID == "" || row.GenerationID == "" || row.SourceUID == "" {
			continue
		}
		gk := groupKey{row.EvidenceSource, row.ScopeID, row.GenerationID}
		groups[gk] = append(groups[gk], row.SourceUID)
	}

	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}

	var (
		edgesSeen = len(rows)
		scopes    int
	)
	sourceSet := map[string]struct{}{}
	for gk, uids := range groups {
		sourceSet[gk.evidenceSource] = struct{}{}
		if err := b.Ledger.RecordProjectedSources(
			ctx, gk.evidenceSource, gk.scopeID, gk.generationID, uids, now,
		); err != nil {
			return fmt.Errorf("projected source edge backfill record for (%s, %s, %s): %w",
				gk.evidenceSource, gk.scopeID, gk.generationID, err)
		}
		scopes++
	}

	// Mark every enumerated source complete so the next startup skips it. A
	// source with zero graph edges (enumerated but absent from sourceSet) has
	// nothing to backfill and is complete too; marking it here avoids
	// re-enumerating the whole graph on every startup for that source.
	for _, src := range toBackfill {
		key := projectedSourceEdgeBackfillKey(src)
		if err := b.StateMarker.MarkComplete(ctx, key, now); err != nil {
			return fmt.Errorf("projected source edge backfill mark complete for source %q: %w", src, err)
		}
	}

	slog.InfoContext(
		ctx, "projected source edge backfill complete",
		"edges_seen", edgesSeen,
		"sources_backfilled", len(sourceSet),
		"scopes", scopes,
	)

	return nil
}
