// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// apiEndpointRepoPathPresenceKey forwards to
// [gpphase.APIEndpointRepoPathPresenceKey].
func apiEndpointRepoPathPresenceKey(repoID, path string) string {
	return gpphase.APIEndpointRepoPathPresenceKey(repoID, path)
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

// handlesRouteEndpointPresenceKey and filterRowsByTargetPresence moved to
// [worker] (issue #6061): the symbol→runtime presence gate that called them
// (symbolRuntimePresenceGate, filterRowsByReadiness) moved there in H5, and
// worker now calls [gpphase.HandlesRouteEndpointPresenceKey] and its own
// filterRowsByTargetPresence directly instead of through a root forwarder.
