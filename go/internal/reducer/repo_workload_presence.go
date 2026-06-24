// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"
)

// repoWorkloadPresenceKey synthesizes the repo_id presence uid a committed
// :Workload is recorded under in the GraphProjectionKeyspaceRepoWorkloadPresence
// domain (#2855). RUNS_IN binds a handler Function to every Workload its
// Repository DEFINES, so presence is proven at repo granularity, not per
// workload. It returns an empty string for a blank repo_id, which cannot key a
// presence row and must be skipped by both the publisher and the gate.
func repoWorkloadPresenceKey(repoID string) string {
	return strings.TrimSpace(repoID)
}

// publishRepoWorkloadPresence records repo-keyed presence for the committed
// :Workload nodes so the runs_in projection gate can prove a repo's Workloads
// exist before resolving a Function-[:RUNS_IN]->Workload edge against them
// (#2855). It mirrors publishAPIEndpointRepoPathPresence: same EndpointPresence
// store (#1380), a different keyspace, and a synthesized uid (here the bare
// repo_id). A multi-workload repo emits one WorkloadRow per workload, so the rows
// are deduplicated by repo_id before the upsert — the batched
// INSERT ... ON CONFLICT (keyspace, uid) DO UPDATE rejects a conflict key that
// appears twice in one VALUES list, which would otherwise make the workload
// materialization intent retry forever after its graph write already succeeded.
// FLAG-GATED by a nil writer: when the gate is off (or no workloads committed)
// this is a no-op, so the hot workload commit path carries zero extra write.
func publishRepoWorkloadPresence(
	ctx context.Context,
	writer EndpointPresenceWriter,
	scopeID string,
	generationID string,
	workloadRows []WorkloadRow,
	committedAt time.Time,
) error {
	if writer == nil || len(workloadRows) == 0 {
		return nil
	}
	rows := make([]EndpointPresenceRow, 0, len(workloadRows))
	seen := make(map[string]struct{}, len(workloadRows))
	repoIDs := make([]string, 0, len(workloadRows))
	for _, workload := range workloadRows {
		uid := repoWorkloadPresenceKey(workload.RepoID)
		if uid == "" {
			continue
		}
		if _, exists := seen[uid]; exists {
			continue
		}
		seen[uid] = struct{}{}
		repoIDs = append(repoIDs, uid) // repo-workload uid IS the repo_id
		rows = append(rows, EndpointPresenceRow{
			Keyspace:         GraphProjectionKeyspaceRepoWorkloadPresence,
			UID:              uid,
			ScopeID:          scopeID,
			RepoID:           uid,
			SourceGeneration: generationID,
			CommittedAt:      committedAt,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := writer.Upsert(ctx, rows); err != nil {
		return err
	}
	// Retract the other-generation presence rows for the repos materialized this
	// generation, so a repo whose Workload set shrank stops over-reporting (#2842).
	// A repo that lost ALL its Workloads materializes no row here and so is not
	// retracted by this path; that narrow case stays bounded-safe (the gate cannot
	// resolve an edge against a Workload that no longer exists) and is left to the
	// scope/generation-retention lifecycle.
	return writer.RetractStaleRepoGenerations(
		ctx, GraphProjectionKeyspaceRepoWorkloadPresence, scopeID, generationID, repoIDs,
	)
}

// runsInRepoWorkloadPresenceKey returns the repo_id presence uid for one runs_in
// intent row, reading repo_id from the intent payload (the field
// buildRunsInIntentRows emits) and falling back to RepositoryID. It returns an
// empty string when neither is set, in which case the gate cannot prove presence.
func runsInRepoWorkloadPresenceKey(row SharedProjectionIntentRow) string {
	repoID := payloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	return repoWorkloadPresenceKey(repoID)
}
