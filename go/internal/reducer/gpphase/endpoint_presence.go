// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"time"
)

// EndpointPresenceRow records that one endpoint node uid is committed in the
// canonical graph, keyed by its bounded keyspace. It is the uid-exact,
// cross-scope readiness primitive (issue #1380, ADR #1314 §6/§8): a presence
// row proves the specific node X is committed, which the same-scope/same-
// generation [PhaseState] gate cannot express. CommittedAt is the node
// materializer's commit instant; an empty value defers to the store's clock.
type EndpointPresenceRow struct {
	Keyspace Keyspace
	UID      string
	ScopeID  string
	// RepoID and SourceGeneration are written only by the symbol->runtime
	// presence publishers (#2842) so stale rows can be retracted per repo when
	// a generation re-materializes -- the synthesized uid is a hash (#2844)
	// and no longer carries the repo_id. They are blank for the uid-exact
	// #1380 presence rows, which are retracted by scope/node lifecycle
	// instead. Both are NUL-free (a repo_id and a generation id contain no
	// 0x00), so they are safe in the Postgres text columns.
	RepoID           string
	SourceGeneration string
	CommittedAt      time.Time
}

// EndpointPresenceWriter records and retracts endpoint-node presence. The
// CloudResource and KubernetesWorkload node materializers call Upsert with
// one row per committed node uid (idempotent: re-upserting the same
// (keyspace, uid) converges on one row), and RetractScope removes a scope's
// presence rows so a node retract removes its presence. Implementations MUST
// be safe under concurrent materializer workers (the upsert is ON CONFLICT
// idempotent); the contract forbids reducing workers or batch size to dodge a
// race.
type EndpointPresenceWriter interface {
	Upsert(ctx context.Context, rows []EndpointPresenceRow) error
	RetractScope(ctx context.Context, scopeIDs []string) error
	// RetractStaleRepoGenerations removes a keyspace's presence rows for the
	// given repos whose source_generation differs from generationID (#2842),
	// so a repo's removed or re-pathed endpoints/workloads stop being
	// reported present once the repo re-materializes. It is race-free under
	// concurrent materializer workers: it only deletes rows from OTHER
	// generations, never the current generation's rows that a sibling intent
	// may have just upserted, and deleting an already-removed older row is
	// idempotent. A blank generationID or empty repo set is a no-op.
	RetractStaleRepoGenerations(ctx context.Context, keyspace Keyspace, scopeID, generationID string, repoIDs []string) error
}

// PublishEndpointPresence records uid-exact presence for the committed
// endpoint nodes so the cross-scope secrets/IAM graph projection gate can
// prove a specific node is committed (issue #1380, ADR #1314 §6/§8; moved
// here from the reducer root's publishEndpointPresence, issue #6061). It is
// FLAG-GATED at the call site by a nil writer: when the secrets/IAM graph
// projection feature is off (the default), the node materializers pass a nil
// writer and this is a no-op, so the hot CloudResource / KubernetesWorkload
// node-commit paths carry zero extra write. When enabled, the upsert is
// idempotent (the store conflicts on (keyspace, uid)) and safe under
// concurrent materializer workers, so it never requires reducing workers or
// batch size. Blank uids are skipped.
func PublishEndpointPresence(
	ctx context.Context,
	writer EndpointPresenceWriter,
	keyspace Keyspace,
	scopeID string,
	nodeRows []map[string]any,
	committedAt time.Time,
) error {
	if writer == nil || len(nodeRows) == 0 {
		return nil
	}
	rows := make([]EndpointPresenceRow, 0, len(nodeRows))
	for _, nodeRow := range nodeRows {
		uid, _ := nodeRow["uid"].(string)
		if uid == "" {
			continue
		}
		rows = append(rows, EndpointPresenceRow{
			Keyspace:    keyspace,
			UID:         uid,
			ScopeID:     scopeID,
			CommittedAt: committedAt,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return writer.Upsert(ctx, rows)
}
