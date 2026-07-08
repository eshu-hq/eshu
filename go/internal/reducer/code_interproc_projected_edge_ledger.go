// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// CodeInterprocProjectedEdgeLedger records and enumerates the source Function
// uids of projected TAINT_FLOWS_TO edges so retraction can enumerate uids from
// the ledger instead of scanning the whole :Function label in the graph.
//
// Write ordering invariant: RecordProjectedEdges MUST be called before the
// graph edge write, so the ledger is always a superset of graph edges.
// Over-inclusion is harmless because the anchored Cypher retract WHERE still
// filters; under-inclusion would orphan graph edges.
type CodeInterprocProjectedEdgeLedger interface {
	// RecordProjectedEdges records source Function uids, idempotent on the
	// primary key. Must be called before the corresponding graph edge write.
	RecordProjectedEdges(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		generationID string,
		sourceFunctionUIDs []string,
		updatedAt time.Time,
	) error

	// ListSourceUIDsForScopes returns distinct source Function uids for the
	// given evidence source and scope IDs.
	ListSourceUIDsForScopes(
		ctx context.Context,
		evidenceSource string,
		scopeIDs []string,
	) ([]string, error)

	// ListSourceUIDsForSource returns distinct source Function uids for the
	// given evidence source only.
	ListSourceUIDsForSource(
		ctx context.Context,
		evidenceSource string,
	) ([]string, error)

	// ListStaleSourceUIDs returns source Function uids for the given scope
	// whose generation is not the current generation, up to limit rows.
	ListStaleSourceUIDs(
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

	// PruneForSource removes all ledger rows for the given evidence source.
	PruneForSource(
		ctx context.Context,
		evidenceSource string,
	) error

	// PruneStale removes ledger rows for the given evidence source and scope
	// whose generation is not the current generation.
	PruneStale(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		currentGenerationID string,
	) error

	// LedgerHasRowsForSource returns true when at least one row exists for the
	// given evidence source. Used by the backfill orchestrator to determine
	// whether a source already has a seeded ledger (idempotent once).
	LedgerHasRowsForSource(
		ctx context.Context,
		evidenceSource string,
	) (bool, error)
}
