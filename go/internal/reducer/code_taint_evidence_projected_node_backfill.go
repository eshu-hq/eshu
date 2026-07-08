// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ProjectedTaintNodeRow is one enumerated CodeTaintEvidence node from the graph,
// carrying the identity fields the backfill ledger needs.
type ProjectedTaintNodeRow struct {
	EvidenceSource string
	ScopeID        string
	GenerationID   string
	NodeUID        string
}

// codeTaintNodeBackfillQuerier is the read surface the backfill orchestrator
// needs: a fast label count and a labeled full enumeration.
type codeTaintNodeBackfillQuerier interface {
	CountCodeTaintEvidenceNodes(ctx context.Context) (int64, error)
	EnumerateProjectedTaintNodes(ctx context.Context, evidenceSources []string) ([]ProjectedTaintNodeRow, error)
}

// CodeTaintEvidenceProjectedNodeBackfiller seeds the
// CodeTaintEvidenceProjectedNodeLedger from existing graph CodeTaintEvidence
// nodes so the ledger is a superset of graph nodes at deploy time. Nodes
// projected BEFORE this deploy have no ledger rows; the backfiller enumerates
// them once, idempotent per evidence source.
//
// Completion is tracked via a durable CodeValueFlowBackfillStateMarker so a
// partial backfill that records some groups then errors does not skip the
// source on the next startup.
type CodeTaintEvidenceProjectedNodeBackfiller struct {
	// Reader is the backfill graph-read surface (CountCodeTaintEvidenceNodes +
	// EnumerateProjectedTaintNodes). Production wiring passes a
	// CodeTaintEvidenceProjectedNodeBackfillReader backed by GraphQueryRunner.
	Reader codeTaintNodeBackfillQuerier

	// Ledger is the durable projected-node store. When nil, Run is a no-op.
	Ledger CodeTaintEvidenceProjectedNodeLedger

	// StateMarker is the durable completion-marker store. When nil, Run falls
	// back to checking ledger rows (existing behavior) so the no-migration path
	// stays backward-compatible.
	StateMarker CodeValueFlowBackfillStateMarker

	// EvidenceSources is the set of evidence_source values to backfill.
	EvidenceSources []string

	// Now returns the current time (injected for test determinism).
	Now func() time.Time
}

// CodeTaintEvidenceProjectedNodeBackfillReader provides the two graph-read
// capabilities the backfill orchestrator needs, backed by GraphQueryRunner.
type CodeTaintEvidenceProjectedNodeBackfillReader struct {
	Graph GraphQueryRunner
}

// CountCodeTaintEvidenceNodes uses the label count to hit NornicDB's label index
// fast path. Do NOT add a WHERE clause — that would defeat the index path and
// make the count guard a full scan instead of a ~0.01s index lookup.
func (r CodeTaintEvidenceProjectedNodeBackfillReader) CountCodeTaintEvidenceNodes(ctx context.Context) (int64, error) {
	if r.Graph == nil {
		return 0, fmt.Errorf("backfill reader requires graph query runner")
	}
	rows, err := r.Graph.Run(ctx, `MATCH (n:CodeTaintEvidence) RETURN count(n) AS c`, nil)
	if err != nil {
		return 0, fmt.Errorf("count code taint evidence nodes: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return anyToInt64(rows[0]["c"]), nil
}

// EnumerateProjectedTaintNodes runs the one-time full scan of
// CodeTaintEvidence nodes for the given evidence sources. This only runs when
// the count guard says nodes exist and the per-source ledger check says a
// source needs backfill.
func (r CodeTaintEvidenceProjectedNodeBackfillReader) EnumerateProjectedTaintNodes(
	ctx context.Context, evidenceSources []string,
) ([]ProjectedTaintNodeRow, error) {
	if r.Graph == nil {
		return nil, fmt.Errorf("backfill reader requires graph query runner")
	}
	rows, err := r.Graph.Run(
		ctx,
		`MATCH (n:CodeTaintEvidence)
WHERE n.evidence_source IN $evidence_sources
RETURN n.uid AS node_uid,
       n.scope_id AS scope_id,
       n.generation_id AS generation_id,
       n.evidence_source AS evidence_source`,
		map[string]any{"evidence_sources": evidenceSources},
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate projected taint nodes: %w", err)
	}
	var out []ProjectedTaintNodeRow
	for _, row := range rows {
		nodeUID := anyToString(row["node_uid"])
		scopeID := anyToString(row["scope_id"])
		genID := anyToString(row["generation_id"])
		evSrc := anyToString(row["evidence_source"])
		if nodeUID == "" || scopeID == "" || genID == "" || evSrc == "" {
			continue
		}
		out = append(out, ProjectedTaintNodeRow{
			EvidenceSource: evSrc,
			ScopeID:        scopeID,
			GenerationID:   genID,
			NodeUID:        nodeUID,
		})
	}
	return out, nil
}

// Run seeds the ledger from existing graph CodeTaintEvidence nodes. It is a
// one-time, idempotent startup backfill:
//  1. Nil guard: nils return nil (backward-compat, test-safe).
//  2. Count guard: if graph has zero CodeTaintEvidence nodes → return nil.
//  3. Per-source check: if the ledger already has rows for a source → skip it.
//  4. Only remaining sources trigger the one-time enumeration + grouped
//     RecordProjectedNodes.
func (b CodeTaintEvidenceProjectedNodeBackfiller) Run(ctx context.Context) error {
	if b.Reader == nil || b.Ledger == nil {
		return nil
	}

	count, err := b.Reader.CountCodeTaintEvidenceNodes(ctx)
	if err != nil {
		return fmt.Errorf("backfill taint node count: %w", err)
	}
	if count == 0 {
		return nil
	}

	// Determine which sources still need backfill.
	var toBackfill []string
	for _, src := range b.EvidenceSources {
		done, err := b.isSourceComplete(ctx, src)
		if err != nil {
			return fmt.Errorf("backfill taint node check completion for source %q: %w", src, err)
		}
		if !done {
			toBackfill = append(toBackfill, src)
		}
	}
	if len(toBackfill) == 0 {
		return nil
	}

	// Enumerate all nodes for the sources that need backfill.
	rows, err := b.Reader.EnumerateProjectedTaintNodes(ctx, toBackfill)
	if err != nil {
		return fmt.Errorf("backfill taint node enumerate: %w", err)
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
		groups[gk] = append(groups[gk], row.NodeUID)
	}

	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}

	var (
		nodesSeen         = len(rows)
		sourcesBackfilled int
		scopes            int
	)
	sourceSet := map[string]struct{}{}
	for gk, uids := range groups {
		sourceSet[gk.evidenceSource] = struct{}{}
		if err := b.Ledger.RecordProjectedNodes(
			ctx, gk.evidenceSource, gk.scopeID, gk.generationID, uids, now,
		); err != nil {
			return fmt.Errorf("backfill record taint nodes for (%s, %s, %s): %w",
				gk.evidenceSource, gk.scopeID, gk.generationID, err)
		}
		scopes++
	}
	sourcesBackfilled = len(sourceSet)

	// Mark each source complete so the next startup skips it.
	if b.StateMarker != nil {
		for _, src := range toBackfill {
			key := codeTaintNodeBackfillKey(src)
			if err := b.StateMarker.MarkComplete(ctx, key, now); err != nil {
				return fmt.Errorf("backfill mark complete for source %q: %w", src, err)
			}
		}
	}

	slog.InfoContext(
		ctx, "code taint evidence projected node backfill complete",
		"nodes_seen", nodesSeen,
		"sources_backfilled", sourcesBackfilled,
		"scopes", scopes,
	)

	return nil
}

// codeTaintNodeBackfillKey returns the stable backfill-state marker key for the
// taint node ledger backfiller.
func codeTaintNodeBackfillKey(evidenceSource string) string {
	return "code_taint_evidence_projected_node:" + evidenceSource
}

// isSourceComplete returns true when the source's backfill has been marked
// complete. When the StateMarker is nil, it falls back to checking whether the
// ledger has any rows for the source.
func (b CodeTaintEvidenceProjectedNodeBackfiller) isSourceComplete(ctx context.Context, evidenceSource string) (bool, error) {
	if b.StateMarker != nil {
		return b.StateMarker.IsComplete(ctx, codeTaintNodeBackfillKey(evidenceSource))
	}
	return b.Ledger.LedgerHasRowsForSource(ctx, evidenceSource)
}
