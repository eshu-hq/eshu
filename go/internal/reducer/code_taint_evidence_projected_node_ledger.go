// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// CodeTaintEvidenceProjectedNodeLedger records and enumerates the uids of
// projected CodeTaintEvidence nodes so retraction can enumerate uids from the
// ledger instead of scanning the whole :CodeTaintEvidence label in the graph.
//
// Write ordering invariant: RecordProjectedNodes MUST be called before the
// graph node write, so the ledger is always a superset of graph nodes.
// Over-inclusion is harmless because the anchored Cypher retract WHERE still
// filters; under-inclusion would orphan graph nodes.
type CodeTaintEvidenceProjectedNodeLedger interface {
	// RecordProjectedNodes records node uids, idempotent on the primary key.
	// Must be called before the corresponding graph node write.
	RecordProjectedNodes(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		generationID string,
		nodeUIDs []string,
		updatedAt time.Time,
	) error

	// ListNodeUIDsForScopes returns distinct node uids for the given evidence
	// source and scope IDs.
	ListNodeUIDsForScopes(
		ctx context.Context,
		evidenceSource string,
		scopeIDs []string,
	) ([]string, error)

	// ListStaleNodeUIDs returns node uids for the given scope whose generation
	// is not the current generation, up to limit rows.
	ListStaleNodeUIDs(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		currentGenerationID string,
		limit int,
	) ([]string, error)

	// PruneForScopes removes all ledger rows for the given evidence source and
	// scope IDs.
	PruneForScopes(
		ctx context.Context,
		evidenceSource string,
		scopeIDs []string,
	) error

	// PruneStaleForUIDs removes ledger rows for the given evidence source,
	// scope, and stale generation whose node_uid is in the provided list.
	// Only the uids that were actually retracted from the graph are pruned.
	PruneStaleForUIDs(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		currentGenerationID string,
		uids []string,
	) error

	// LedgerHasRowsForSource returns true when at least one row exists for the
	// given evidence source. Used by the backfill orchestrator to determine
	// whether a source already has a seeded ledger (idempotent once).
	LedgerHasRowsForSource(
		ctx context.Context,
		evidenceSource string,
	) (bool, error)
}
