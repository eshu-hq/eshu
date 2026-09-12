// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// apiEndpointRepoPathPresenceKeySeparator joins repo_id and path in the presence
// uid HASH INPUT only. The NUL byte never appears in a repo_id or route path, so
// the (repo_id, path) pair hashes to exactly one digest with no collision and no
// separator ambiguity. It is only ever a hash input — never stored — so the
// 0x00 byte never reaches Postgres.
const apiEndpointRepoPathPresenceKeySeparator = "\x00"

// apiEndpointRepoPathPresenceKeyPrefix labels the synthesized presence uid so it
// is self-describing in the graph_endpoint_presence table.
const apiEndpointRepoPathPresenceKeyPrefix = "api-endpoint-presence:"

// APIEndpointRepoPathPresenceKey synthesizes the (repo_id, path) presence uid
// an :Endpoint node is recorded under in the [KeyspaceAPIEndpointRepoPath]
// presence domain (#2809; moved here from the reducer root's
// apiEndpointRepoPathPresenceKey, issue #6061). It returns an empty string
// when either component is blank, because a blank component cannot key a
// presence row and must be skipped by both the publisher and the gate.
//
// The uid is a SHA-256 hex digest, not a raw repo_id+separator+path join: the
// uid is written to the Postgres text graph_endpoint_presence.uid column, and a
// raw join embeds the 0x00 separator byte, which Postgres rejects for text
// (SQLSTATE 22021) — dead-lettering workload materialization for every
// endpoint-exposing repo (#2844 regression). Hashing keeps the key
// collision-free and separator-unambiguous while staying Postgres-safe (hex,
// no control bytes). Publisher and gate both call this function, so they agree.
func APIEndpointRepoPathPresenceKey(repoID, path string) string {
	repoID = strings.TrimSpace(repoID)
	path = strings.TrimSpace(path)
	if repoID == "" || path == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(repoID + apiEndpointRepoPathPresenceKeySeparator + path))
	return apiEndpointRepoPathPresenceKeyPrefix + hex.EncodeToString(digest[:16])
}

// HandlesRouteEndpointPresenceKey returns the (repo_id, path) presence uid for
// one handles_route intent row, reading the repo_id and path from the intent
// payload (the fields buildHandlesRouteIntentRows emits; moved here from the
// reducer root's handlesRouteEndpointPresenceKey, issue #6061). It returns an
// empty string when either is missing, in which case the gate cannot prove
// presence and defers the row.
func HandlesRouteEndpointPresenceKey(row sharedintent.Row) string {
	repoID := payloadcore.PayloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	path := payloadcore.PayloadStr(row.Payload, "path")
	return APIEndpointRepoPathPresenceKey(repoID, path)
}

// RepoWorkloadPresenceKey synthesizes the repo_id presence uid a committed
// :Workload is recorded under in the [KeyspaceRepoWorkloadPresence] domain
// (#2855; moved here from the reducer root's repoWorkloadPresenceKey, issue
// #6061). RUNS_IN binds a handler Function to every Workload its Repository
// DEFINES, so presence is proven at repo granularity, not per workload. It
// returns an empty string for a blank repo_id, which cannot key a presence
// row and must be skipped by both the publisher and the gate.
func RepoWorkloadPresenceKey(repoID string) string {
	return strings.TrimSpace(repoID)
}

// RunsInRepoWorkloadPresenceKey returns the repo_id presence uid for one
// runs_in intent row, reading repo_id from the intent payload (the field
// buildRunsInIntentRows emits) and falling back to RepositoryID (moved here
// from the reducer root's runsInRepoWorkloadPresenceKey, issue #6061). It
// returns an empty string when neither is set, in which case the gate cannot
// prove presence.
func RunsInRepoWorkloadPresenceKey(row sharedintent.Row) string {
	repoID := payloadcore.PayloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	return RepoWorkloadPresenceKey(repoID)
}

// WorkloadMaterializationRepoReadinessKey builds the deterministic per-repo
// readiness key that the workload-materialization handler publishes and that
// the symbol→runtime shared-projection domains (handles_route, runs_in)
// reconstruct to find it (#2891; moved here from the reducer root's
// workloadMaterializationRepoReadinessKey, issue #6061). The two stages run
// under DIFFERENT source runs — the HANDLES_ROUTE/RUNS_IN intents carry the
// CODE stage's source_run, while the workload-materialization phase commits
// under the WORKLOAD stage's run — so an exact (scope, acceptance_unit,
// source_run, generation, keyspace) match between the consumer's
// intent-derived key and the publisher's intent-derived key can NEVER align.
// Both sides instead derive the key from only (scopeID, repoID,
// generationID): the repo id is the acceptance unit, and the generation id
// doubles as the source run so a code-stage row can reconstruct it without
// knowing the workload stage's run. The keyspace is service_uid because the
// workload_materialization phase commits the Endpoint and Workload nodes
// under the service identity domain. Defining the key in ONE function used by
// publisher and consumer guarantees they cannot drift.
func WorkloadMaterializationRepoReadinessKey(scopeID, repoID, generationID string) PhaseKey {
	generationID = strings.TrimSpace(generationID)
	return PhaseKey{
		ScopeID:          strings.TrimSpace(scopeID),
		AcceptanceUnitID: strings.TrimSpace(repoID),
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         KeyspaceServiceUID,
	}
}
