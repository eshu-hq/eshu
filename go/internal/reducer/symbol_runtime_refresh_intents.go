// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ExtractSymbolRuntimeIntentRows builds every symbol->runtime shared-projection
// intent row -- handles_route, runs_in, and invokes_cloud_action -- from
// repository and file envelopes, with no backend dependency. It is modeled on
// ExtractAllCodeRelationshipRows (code_call_materialization_extract.go): it
// builds the unexported codeEntityIndex internally via
// extractAllCodeRelationshipRowsWithIndex and never returns it, so a caller
// outside this package can drive the real production intent builders
// (buildHandlesRouteIntentRows, buildRunsInIntentRows,
// buildInvokesCloudActionIntentRows, via their shared entry point
// buildSymbolRuntimeIntentRows below) without needing a graph backend,
// Postgres, or a clock -- createdAt is injected by the caller.
//
// It lives here, beside buildSymbolRuntimeIntentRows, rather than in its own
// symbol_runtime_extract.go sibling file (the shape
// code_call_materialization_extract.go would otherwise suggest), because
// internal/reducer is at its grandfathered go-dir-gate file cap
// (scripts/lib/dirgate-grandfather.tsv): a new non-test file cannot land here
// without lowering that count, which this function does not do.
//
// The returned slice is MIXED and callers MUST account for both shapes:
//
//   - Per-edge rows, one per resolved HANDLES_ROUTE/RUNS_IN/INVOKES_CLOUD_ACTION
//     binding, whose payload carries no "action" key (implicitly "upsert").
//   - Exactly one per-repo, per-domain repo-wide refresh row
//     (payload "action": "refresh", "intent_type": "repo_refresh"), emitted by
//     buildRepoWideRetractRefreshIntents below alongside the per-edge rows it
//     fences. A caller that wants only edges MUST filter on
//     payload["action"] == "upsert" or absent, mirroring the production
//     filterUpsertRows gate (shared_projection_readiness.go:246); every
//     per-edge row here also carries retract_via_refresh=true, pairing it with
//     its domain's refresh row.
//
// Rows for all THREE domains are returned together in one call, distinguished
// by ProjectionDomain (DomainHandlesRoute, DomainRunsIn,
// DomainInvokesCloudAction) -- there is no per-domain variant of this seam,
// matching the single shared production entry point.
func ExtractSymbolRuntimeIntentRows(
	envelopes []facts.Envelope,
	generationID string,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	_, _, _, _, entityIndex, _ := extractAllCodeRelationshipRowsWithIndex(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, generationID)
	return buildSymbolRuntimeIntentRows(envelopes, entityIndex, contextByRepoID, createdAt)
}

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
				"intent_type":     RepoRefreshIntentType,
				"action":          repoRefreshAction,
				"evidence_source": evidenceSource,
			},
			CreatedAt: createdAt,
		}))
	}
	return intents
}
