// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ProjectedTaintEdgeRow is one enumerated TAINT_FLOWS_TO edge from the
// graph, carrying the identity fields the backfill ledger needs.
type ProjectedTaintEdgeRow struct {
	EvidenceSource    string
	ScopeID           string
	GenerationID      string
	SourceFunctionUID string
}

// codeInterprocBackfillQuerier is the read surface the backfill orchestrator
// needs: a fast bare-type count and a labeled full enumeration. Production
// code satisfies it via CodeInterprocProjectedEdgeBackfillReader (backed by
// GraphQueryRunner); tests supply a fake.
type codeInterprocBackfillQuerier interface {
	CountTaintFlowsToEdges(ctx context.Context) (int64, error)
	EnumerateProjectedTaintEdges(ctx context.Context, evidenceSources []string) ([]ProjectedTaintEdgeRow, error)
}

// CodeInterprocProjectedEdgeBackfiller seeds the CodeInterprocProjectedEdgeLedger
// from existing graph TAINT_FLOWS_TO edges so the ledger is a superset of graph
// edges at deploy time. Edges projected BEFORE this deploy have no ledger rows;
// the backfiller enumerates them once, idempotent per evidence source.
type CodeInterprocProjectedEdgeBackfiller struct {
	// Reader is the backfill graph-read surface (CountTaintFlowsToEdges +
	// EnumerateProjectedTaintEdges). Production wiring passes a
	// CodeInterprocProjectedEdgeBackfillReader backed by GraphQueryRunner.
	Reader codeInterprocBackfillQuerier

	// Ledger is the durable projected-edge store. When nil, Run is a no-op.
	Ledger CodeInterprocProjectedEdgeLedger

	// EvidenceSources is the set of evidence_source values to backfill.
	EvidenceSources []string

	// Now returns the current time (injected for test determinism).
	Now func() time.Time
}

// CodeInterprocProjectedEdgeBackfillReader provides the two graph-read
// capabilities the backfill orchestrator needs, backed by GraphQueryRunner.
// It is the production reader; tests supply a fake implementing
// codeInterprocBackfillQuerier.
type CodeInterprocProjectedEdgeBackfillReader struct {
	Graph GraphQueryRunner
}

// CountTaintFlowsToEdges uses the bare relationship-type count to hit
// NornicDB's relationship-type-index fast path. Do NOT add labels or a
// WHERE clause — those would defeat the index path and make the count
// guard a full scan instead of a ~0.01s index lookup.
func (r CodeInterprocProjectedEdgeBackfillReader) CountTaintFlowsToEdges(ctx context.Context) (int64, error) {
	if r.Graph == nil {
		return 0, fmt.Errorf("backfill reader requires graph query runner")
	}
	rows, err := r.Graph.Run(ctx, `MATCH ()-[r:TAINT_FLOWS_TO]->() RETURN count(r) AS c`, nil)
	if err != nil {
		return 0, fmt.Errorf("count taint flows-to edges: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return anyToInt64(rows[0]["c"]), nil
}

// EnumerateProjectedTaintEdges runs the one-time full scan of TAINT_FLOWS_TO
// edges for the given evidence sources. This only runs when the count guard
// says edges exist and the per-source ledger check says a source needs
// backfill.
func (r CodeInterprocProjectedEdgeBackfillReader) EnumerateProjectedTaintEdges(
	ctx context.Context, evidenceSources []string,
) ([]ProjectedTaintEdgeRow, error) {
	if r.Graph == nil {
		return nil, fmt.Errorf("backfill reader requires graph query runner")
	}
	rows, err := r.Graph.Run(
		ctx,
		`MATCH (s:Function)-[r:TAINT_FLOWS_TO]->(:Function)
WHERE r.evidence_source IN $evidence_sources
RETURN s.uid AS source_function_uid,
       r.scope_id AS scope_id,
       r.generation_id AS generation_id,
       r.evidence_source AS evidence_source`,
		map[string]any{"evidence_sources": evidenceSources},
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate projected taint edges: %w", err)
	}
	var out []ProjectedTaintEdgeRow
	for _, row := range rows {
		sourceUID := anyToString(row["source_function_uid"])
		scopeID := anyToString(row["scope_id"])
		genID := anyToString(row["generation_id"])
		evSrc := anyToString(row["evidence_source"])
		if sourceUID == "" || scopeID == "" || genID == "" || evSrc == "" {
			continue
		}
		out = append(out, ProjectedTaintEdgeRow{
			EvidenceSource:    evSrc,
			ScopeID:           scopeID,
			GenerationID:      genID,
			SourceFunctionUID: sourceUID,
		})
	}
	return out, nil
}

// Run seeds the ledger from existing graph TAINT_FLOWS_TO edges. It is a
// one-time, idempotent startup backfill:
//  1. Nil guard: nils return nil (backward-compat, test-safe).
//  2. Count guard: if graph has zero TAINT_FLOWS_TO edges → return nil.
//  3. Per-source check: if the ledger already has rows for a source → skip it.
//  4. Only remaining sources trigger the one-time enumeration + grouped
//     RecordProjectedEdges.
func (b CodeInterprocProjectedEdgeBackfiller) Run(ctx context.Context) error {
	if b.Reader == nil || b.Ledger == nil {
		return nil
	}

	count, err := b.Reader.CountTaintFlowsToEdges(ctx)
	if err != nil {
		return fmt.Errorf("backfill count: %w", err)
	}
	if count == 0 {
		return nil
	}

	// Determine which sources still need backfill.
	var toBackfill []string
	for _, src := range b.EvidenceSources {
		has, err := b.Ledger.LedgerHasRowsForSource(ctx, src)
		if err != nil {
			return fmt.Errorf("backfill check ledger for source %q: %w", src, err)
		}
		if !has {
			toBackfill = append(toBackfill, src)
		}
	}
	if len(toBackfill) == 0 {
		return nil
	}

	// Enumerate all edges for the sources that need backfill.
	rows, err := b.Reader.EnumerateProjectedTaintEdges(ctx, toBackfill)
	if err != nil {
		return fmt.Errorf("backfill enumerate: %w", err)
	}

	// Group by (evidence_source, scope_id, generation_id) and record.
	type groupKey struct {
		evidenceSource string
		scopeID        string
		generationID   string
	}
	groups := make(map[groupKey][]string)
	for _, row := range rows {
		gk := groupKey{row.EvidenceSource, row.ScopeID, row.GenerationID}
		groups[gk] = append(groups[gk], row.SourceFunctionUID)
	}

	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}

	var (
		edgesSeen         = len(rows)
		sourcesBackfilled int
		scopes            int
	)
	sourceSet := map[string]struct{}{}
	for gk, uids := range groups {
		sourceSet[gk.evidenceSource] = struct{}{}
		if err := b.Ledger.RecordProjectedEdges(
			ctx, gk.evidenceSource, gk.scopeID, gk.generationID, uids, now,
		); err != nil {
			return fmt.Errorf("backfill record edges for (%s, %s, %s): %w",
				gk.evidenceSource, gk.scopeID, gk.generationID, err)
		}
		scopes++
	}
	sourcesBackfilled = len(sourceSet)

	slog.InfoContext(
		ctx, "code interproc projected edge backfill complete",
		"edges_seen", edgesSeen,
		"sources_backfilled", sourcesBackfilled,
		"scopes", scopes,
	)

	return nil
}

// anyToInt64 converts an any to int64, or returns 0.
func anyToInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}
