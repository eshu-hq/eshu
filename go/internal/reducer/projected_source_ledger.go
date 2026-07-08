// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// ProjectedSourceLedger records and enumerates the source-node uids of
// projected CloudResource edges so a scoped retraction can enumerate uids from
// the ledger instead of scanning the whole :CloudResource label in the graph.
// It is the generic, multi-evidence-source counterpart of
// CodeInterprocProjectedEdgeLedger: the same superset-ledger pattern, but
// keyed by an arbitrary evidence_source rather than one hardcoded edge kind,
// so the AWS, Azure, GCP, and observability-coverage edge writers can all
// share one durable ledger table (see postgres.ProjectedSourceEdgeStore).
//
// Write ordering invariant: RecordProjectedSources MUST be called before the
// graph edge write, so the ledger is always a superset of graph edges.
// Over-inclusion is harmless because the anchored Cypher retract WHERE still
// filters by scope_id and evidence_source; under-inclusion would orphan graph
// edges that the retract can no longer reach.
type ProjectedSourceLedger interface {
	// RecordProjectedSources records source-node uids for the given evidence
	// source, scope, and generation, idempotent on the ledger's primary key.
	// Callers MUST call this before the corresponding graph edge write
	// completes.
	RecordProjectedSources(
		ctx context.Context,
		evidenceSource string,
		scopeID string,
		generationID string,
		sourceUIDs []string,
		updatedAt time.Time,
	) error

	// ListSourceUIDsForScopes returns the distinct source-node uids recorded
	// for the given evidence source and scope IDs. Results from one evidence
	// source never leak into another.
	ListSourceUIDsForScopes(
		ctx context.Context,
		evidenceSource string,
		scopeIDs []string,
	) ([]string, error)

	// PruneForScopes removes all ledger rows for the given evidence source and
	// scope IDs. Callers MUST call this only after the corresponding anchored
	// retract has completed, so a failed retract can be retried against the
	// still-intact ledger.
	PruneForScopes(
		ctx context.Context,
		evidenceSource string,
		scopeIDs []string,
	) error
}

// sourceUIDsFromRowsByKey extracts distinct string values stored under key
// from a batch of edge rows, skipping rows where the key is absent or not a
// string. It is the shared helper for every CloudResource-edge-family handler
// (AWS, Azure, GCP relationship rows key their source uid as "source_uid";
// observability coverage rows key it as "observability_uid") so each handler
// records the same source-uid set into the ledger that it wrote to the graph.
func sourceUIDsFromRowsByKey(rows []map[string]any, key string) []string {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if uid, ok := row[key].(string); ok && uid != "" {
			seen[uid] = struct{}{}
		}
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	return uids
}
