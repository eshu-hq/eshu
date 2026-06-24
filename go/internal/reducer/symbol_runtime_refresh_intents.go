// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// buildSymbolRuntimeIntentRows builds every symbol→runtime shared-projection
// intent (handles_route, runs_in, invokes_cloud_action) for one materialization
// pass. For each domain it emits the per-edge rows and, paired in the same pass,
// the per-repo refresh intents that own the domain's single repo-wide retract
// (#2898/#2910). Keeping emission in one helper preserves the paired-emission
// invariant the worker's refresh fence relies on and keeps the materialization
// orchestrator small.
func buildSymbolRuntimeIntentRows(
	envelopes []facts.Envelope,
	entityIndex codeEntityIndex,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	handlesRouteRows := markRowsRetractViaRefresh(buildHandlesRouteIntentRows(
		envelopes, entityIndex, contextByRepoID, createdAt, handlesRouteEvidenceSource,
	))
	runsInRows := markRowsRetractViaRefresh(buildRunsInIntentRows(
		envelopes, entityIndex, contextByRepoID, createdAt, runsInEvidenceSource,
	))
	invokesCloudActionRows := markRowsRetractViaRefresh(buildInvokesCloudActionIntentRows(
		envelopes, entityIndex, contextByRepoID, createdAt, invokesCloudActionEvidenceSource,
	))

	rows := make([]SharedProjectionIntentRow, 0,
		len(handlesRouteRows)+len(runsInRows)+len(invokesCloudActionRows))
	rows = append(rows, buildRepoWideRetractRefreshIntents(
		DomainHandlesRoute, handlesRouteRows, contextByRepoID, createdAt, handlesRouteEvidenceSource,
	)...)
	rows = append(rows, handlesRouteRows...)
	rows = append(rows, buildRepoWideRetractRefreshIntents(
		DomainRunsIn, runsInRows, contextByRepoID, createdAt, runsInEvidenceSource,
	)...)
	rows = append(rows, runsInRows...)
	rows = append(rows, buildRepoWideRetractRefreshIntents(
		DomainInvokesCloudAction, invokesCloudActionRows, contextByRepoID, createdAt, invokesCloudActionEvidenceSource,
	)...)
	rows = append(rows, invokesCloudActionRows...)
	return rows
}

// buildRepoWideRetractRefreshIntents emits one whole-scope refresh intent per
// repository that has at least one per-edge intent in perEdgeRows, for a
// repo-wide-retract domain (handles_route, runs_in, invokes_cloud_action). The
// refresh intent owns the domain's single repo-wide retract; the generic worker
// fences the per-edge writes behind it so a repo whose edges span partitions no
// longer loses edges to a per-partition repo-wide retract (#2898/#2910).
//
// It MUST be emitted in the same materialization pass as the per-edge intents so
// every authoritative per-edge row for a (repo, source_run) has a paired refresh
// intent — that pairing is what lets the worker safely treat "refresh not yet
// completed" as "refresh still pending" rather than "no refresh exists". The
// refresh intent carries no edge: its action is repoRefreshAction, so
// filterUpsertRows drops it from writes, and it is exempt from the endpoint
// presence gate.
func buildRepoWideRetractRefreshIntents(
	domain string,
	perEdgeRows []SharedProjectionIntentRow,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []SharedProjectionIntentRow {
	if !domainHasRepoWideRetract(domain) || len(perEdgeRows) == 0 || len(contextByRepoID) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, row := range perEdgeRows {
		repoID := sharedProjectionRowRepoID(row)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	intents := make([]SharedProjectionIntentRow, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: domain,
			PartitionKey:     repoWideRetractRefreshPartitionKey(domain, repoID),
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repoID),
			RepositoryID:     repoID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload: map[string]any{
				"repo_id":         repoID,
				"intent_type":     repoRefreshIntentType,
				"action":          repoRefreshAction,
				"evidence_source": evidenceSource,
			},
			CreatedAt: createdAt,
		}))
	}
	return intents
}
