// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
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

// apiEndpointRepoPathPresenceKey synthesizes the (repo_id, path) presence uid an
// :Endpoint node is recorded under in the GraphProjectionKeyspaceAPIEndpointRepoPath
// presence domain (#2809). It returns an empty string when either component is
// blank, because a blank component cannot key a presence row and must be skipped
// by both the publisher and the gate.
//
// The uid is a SHA-256 hex digest, not a raw repo_id+separator+path join: the
// uid is written to the Postgres text graph_endpoint_presence.uid column, and a
// raw join embeds the 0x00 separator byte, which Postgres rejects for text
// (SQLSTATE 22021) — dead-lettering workload materialization for every
// endpoint-exposing repo (#2844 regression). Hashing keeps the key
// collision-free and separator-unambiguous while staying Postgres-safe (hex,
// no control bytes). Publisher and gate both call this function, so they agree.
func apiEndpointRepoPathPresenceKey(repoID, path string) string {
	repoID = strings.TrimSpace(repoID)
	path = strings.TrimSpace(path)
	if repoID == "" || path == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(repoID + apiEndpointRepoPathPresenceKeySeparator + path))
	return apiEndpointRepoPathPresenceKeyPrefix + hex.EncodeToString(digest[:16])
}

// publishAPIEndpointRepoPathPresence records property-keyed (repo_id, path)
// presence for the committed :Endpoint nodes so the handles_route projection
// gate can prove a specific endpoint exists before resolving a
// Function-[:HANDLES_ROUTE]->Endpoint edge against it (#2809). It mirrors
// publishEndpointPresence (the uid-exact #1380 primitive) but synthesizes the
// presence uid from (repo_id, path) — the identity the handles_route intent
// carries — instead of the workload-scoped endpoint uid. It is FLAG-GATED by a
// nil writer: when endpoint-presence is off (the default) the workload
// materializer passes a nil writer and this is a no-op, so the hot endpoint
// commit path carries zero extra write. When enabled the upsert is idempotent
// (the store conflicts on (keyspace, uid)) and safe under concurrent
// materializer workers. Endpoint rows with a blank repo_id or path are skipped.
//
// The synthesized (repo_id, path) uid collapses many workload-scoped endpoint
// rows onto one presence key: a multi-workload repo can emit several
// APIEndpointRows sharing the same repo_id and route path (the endpoint id
// embeds the workload id, the presence uid does not). Those rows are deduplicated
// by uid before the upsert, because the presence store batches one
// INSERT ... ON CONFLICT (keyspace, uid) DO UPDATE and Postgres rejects the same
// conflict key appearing twice in one VALUES list — which would otherwise make
// the workload materialization intent retry forever after its graph write
// already succeeded.
func publishAPIEndpointRepoPathPresence(
	ctx context.Context,
	writer EndpointPresenceWriter,
	scopeID string,
	generationID string,
	endpointRows []APIEndpointRow,
	committedAt time.Time,
) error {
	if writer == nil || len(endpointRows) == 0 {
		return nil
	}
	rows := make([]EndpointPresenceRow, 0, len(endpointRows))
	seen := make(map[string]struct{}, len(endpointRows))
	repoIDs := make([]string, 0, len(endpointRows))
	repoSeen := make(map[string]struct{}, len(endpointRows))
	for _, endpoint := range endpointRows {
		uid := apiEndpointRepoPathPresenceKey(endpoint.RepoID, endpoint.Path)
		if uid == "" {
			continue
		}
		repoID := strings.TrimSpace(endpoint.RepoID)
		if _, ok := repoSeen[repoID]; !ok && repoID != "" {
			repoSeen[repoID] = struct{}{}
			repoIDs = append(repoIDs, repoID)
		}
		if _, exists := seen[uid]; exists {
			continue
		}
		seen[uid] = struct{}{}
		rows = append(rows, EndpointPresenceRow{
			Keyspace:         GraphProjectionKeyspaceAPIEndpointRepoPath,
			UID:              uid,
			ScopeID:          scopeID,
			RepoID:           repoID,
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
	// Retract this generation's no-longer-present (repo_id, path) endpoints for the
	// repos just materialized, so a removed or re-pathed route stops being reported
	// present (#2842). Race-free: only OTHER generations' rows are deleted.
	return writer.RetractStaleRepoGenerations(
		ctx, GraphProjectionKeyspaceAPIEndpointRepoPath, scopeID, generationID, repoIDs,
	)
}

// handlesRouteEndpointPresenceKey returns the (repo_id, path) presence uid for
// one handles_route intent row, reading the repo_id and path from the intent
// payload (the fields buildHandlesRouteIntentRows emits). It returns an empty
// string when either is missing, in which case the gate cannot prove presence
// and defers the row.
func handlesRouteEndpointPresenceKey(row SharedProjectionIntentRow) string {
	repoID := payloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	path := payloadStr(row.Payload, "path")
	return apiEndpointRepoPathPresenceKey(repoID, path)
}

// filterRowsByTargetPresence splits phase-ready symbol→runtime rows into the rows
// whose target is committed (present) and the rows whose target is absent. It
// backs both the handles_route endpoint-presence gate (#2809) and the runs_in
// repo-workload-presence gate (#2855): the caller supplies the presence keyspace
// and a per-row key function so the same bounded MissingUIDs lookup (ONE call
// over the distinct synthesized uids, never an N+1 per-row probe) serves either
// domain. A nil lookup disables the gate and returns every input row as present,
// so the gated path stays byte-identical to its pre-gate behavior when presence
// is unwired. The caller treats the absent set as TERMINAL (complete, no edge),
// not deferred: the phase gate already proves the repo's targets have all
// committed, so an absent target will never appear.
func filterRowsByTargetPresence(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
	presence EndpointPresenceLookup,
	keyspace GraphProjectionKeyspace,
	keyFor func(SharedProjectionIntentRow) string,
) (present, absent []SharedProjectionIntentRow, err error) {
	if presence == nil || len(rows) == 0 {
		return rows, nil, nil
	}

	keyByRow := make([]string, len(rows))
	seen := make(map[string]struct{}, len(rows))
	uids := make([]string, 0, len(rows))
	for i, row := range rows {
		key := keyFor(row)
		keyByRow[i] = key
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		uids = append(uids, key)
	}
	sort.Strings(uids)

	missing, err := presence.MissingUIDs(ctx, keyspace, uids)
	if err != nil {
		return nil, nil, err
	}
	missingSet := make(map[string]struct{}, len(missing))
	for _, uid := range missing {
		missingSet[uid] = struct{}{}
	}

	present = make([]SharedProjectionIntentRow, 0, len(rows))
	absent = make([]SharedProjectionIntentRow, 0)
	for i, row := range rows {
		key := keyByRow[i]
		// A row with no derivable (repo_id, path) cannot be proven present and
		// cannot anchor a MERGE either, so it joins the absent (terminal) set.
		if key == "" {
			absent = append(absent, row)
			continue
		}
		if _, isMissing := missingSet[key]; isMissing {
			absent = append(absent, row)
			continue
		}
		present = append(present, row)
	}
	return present, absent, nil
}
