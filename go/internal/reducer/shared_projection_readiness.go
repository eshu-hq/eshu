// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// workloadMaterializationRepoReadinessKey builds the deterministic per-repo
// readiness key that the workload-materialization handler publishes and that the
// symbol→runtime shared-projection domains (handles_route, runs_in) reconstruct
// to find it (#2891). The two stages run under DIFFERENT source runs — the
// HANDLES_ROUTE/RUNS_IN intents carry the CODE stage's source_run, while the
// workload-materialization phase commits under the WORKLOAD stage's run — so an
// exact (scope, acceptance_unit, source_run, generation, keyspace) match between
// the consumer's intent-derived key and the publisher's intent-derived key can
// NEVER align. Both sides instead derive the key from only (scopeID, repoID,
// generationID): the repo id is the acceptance unit, and the generation id
// doubles as the source run so a code-stage row can reconstruct it without
// knowing the workload stage's run. The keyspace is service_uid because the
// workload_materialization phase commits the Endpoint and Workload nodes under
// the service identity domain. Defining the key in ONE helper used by publisher
// and consumer guarantees they cannot drift.
func workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID string) GraphProjectionPhaseKey {
	generationID = strings.TrimSpace(generationID)
	return GraphProjectionPhaseKey{
		ScopeID:          strings.TrimSpace(scopeID),
		AcceptanceUnitID: strings.TrimSpace(repoID),
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         GraphProjectionKeyspaceServiceUID,
	}
}

// sharedProjectionReadinessKeyForRow builds the readiness lookup key for one
// intent row. For the symbol→runtime domains (handles_route, runs_in) it uses
// the deterministic per-repo key (#2891) so the code-stage intent finds the
// workload-stage phase row across the source-run boundary. For every other
// domain it falls back to the intent-derived key (graphProjectionPhaseKeyForIntent),
// keeping code_calls and the semantic edge domains byte-identical to their
// pre-#2891 behavior.
func sharedProjectionReadinessKeyForRow(
	domain string,
	row SharedProjectionIntentRow,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	if domain == DomainHandlesRoute || domain == DomainRunsIn {
		repoID := sharedProjectionRowRepoID(row)
		key := workloadMaterializationRepoReadinessKey(row.ScopeID, repoID, row.GenerationID)
		if err := key.Validate(); err != nil {
			return GraphProjectionPhaseKey{}, false
		}
		return key, true
	}
	return graphProjectionPhaseKeyForIntent(row, row.GenerationID, keyspace)
}

// sharedProjectionRowRepoID extracts the repo id a symbol→runtime intent row
// keys its readiness on, using the same precedence the presence-gate key
// functions use (handlesRouteEndpointPresenceKey / runsInRepoWorkloadPresenceKey):
// the payload repo_id first, then RepositoryID. Sharing this precedence keeps the
// readiness repo id and the presence repo id the SAME string for the same row, so
// the phase gate and the presence gate agree on which repo a row belongs to. This
// is also the string the workload-materialization handler publishes its repo-keyed
// phase row under (the APIEndpointRow/WorkloadRow RepoID), so consumer and
// publisher reconstruct an identical key.
func sharedProjectionRowRepoID(row SharedProjectionIntentRow) string {
	repoID := payloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	return repoID
}

// filterRowsByReadiness partitions a domain's pending intent rows into three
// disjoint sets: rows ready to project, rows still blocked on a prerequisite
// graph-projection phase (deferred, re-enqueued), and rows that are terminally
// complete with no edge (drained without a write). Domains without a readiness
// gate pass through as ready. The readiness key is built under the domain's
// prerequisite keyspace (sharedProjectionReadinessKeyspace) so a multi-keyspace
// domain such as handles_route looks up the phase under the keyspace it was
// published in.
//
// endpointPresence adds a SECOND, domain-scoped gate that applies ONLY to
// DomainHandlesRoute (#2809). A handles_route intent carries the repo acceptance
// unit. The repo's single workload-materialization invocation publishes BOTH the
// workload-materialization phase under that repo acceptance unit AND the
// property-keyed (repo_id, path) presence for every :Endpoint it commits, before
// it returns — including the zero-candidate path, which publishes the phase with
// no endpoints. So once a handles_route row passes the phase gate, every endpoint
// that repo will ever produce in this generation already has a presence row.
// Therefore, among phase-ready rows:
//   - an endpoint that is PRESENT projects (readyRows);
//   - an endpoint that is ABSENT will never commit (route-only repo, or a route
//     whose endpoint was not materialized), so the row is TERMINAL — drained with
//     no edge (terminalRows), never deferred. Deferring it would stall the
//     shared-projection backlog forever because no producer can fill the key.
//
// A nil endpointPresence disables this second gate, so every other domain — and
// handles_route itself when presence is unwired — stays byte-identical to its
// pre-#2809 behavior.
func filterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
) (readyRows, blockedRows, terminalRows []SharedProjectionIntentRow, err error) {
	phase, gated := sharedProjectionReadinessPhase(domain)
	if !gated || len(rows) == 0 {
		return rows, nil, nil, nil
	}
	keyspace := sharedProjectionReadinessKeyspace(domain)

	lookup := readinessLookup
	if readinessPrefetch != nil {
		seen := make(map[GraphProjectionPhaseKey]struct{}, len(rows))
		keys := make([]GraphProjectionPhaseKey, 0, len(rows))
		for _, row := range rows {
			key, ok := sharedProjectionReadinessKeyForRow(domain, row, keyspace)
			if !ok {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		resolvedLookup, prefetchErr := readinessPrefetch(ctx, keys, phase)
		if prefetchErr != nil {
			return nil, nil, nil, fmt.Errorf("prefetch graph projection readiness: %w", prefetchErr)
		}
		lookup = resolvedLookup
	}

	if lookup == nil {
		return rows, nil, nil, nil
	}

	readyRows = make([]SharedProjectionIntentRow, 0, len(rows))
	blockedRows = make([]SharedProjectionIntentRow, 0)
	for _, row := range rows {
		key, ok := sharedProjectionReadinessKeyForRow(domain, row, keyspace)
		if !ok {
			continue
		}
		ready, found := lookup(key, phase)
		if !found || !ready {
			blockedRows = append(blockedRows, row)
			continue
		}
		readyRows = append(readyRows, row)
	}

	// Second gate (symbol→runtime domains, #2809/#2855): among the phase-ready
	// rows, terminally drain the ones whose runtime target did not commit —
	// handles_route on its (repo_id, path) :Endpoint, runs_in on its repo's
	// :Workload presence. Because the phase gate already proves workload
	// materialization for the repo is done, an absent target will never appear, so
	// the row is complete with no edge — NOT deferred. A nil presence lookup is a
	// no-op, so every other domain — and these domains when presence is unwired —
	// is unaffected.
	if keyspace, keyFor, gated := symbolRuntimePresenceGate(domain); gated && endpointPresence != nil && len(readyRows) > 0 {
		// The per-repo refresh intent (#2898) carries no endpoint of its own — it
		// exists only to issue the single repo-wide retract — so exempt it from the
		// presence gate. Subjecting it would key on an empty path and drain it as
		// terminal-no-endpoint, so the repo-wide retract would never run.
		refreshReady, edgeReady := splitRepoRefreshRows(readyRows)
		presentRows, absentRows, presenceErr := filterRowsByTargetPresence(ctx, edgeReady, endpointPresence, keyspace, keyFor)
		if presenceErr != nil {
			return nil, nil, nil, fmt.Errorf("look up %s target presence: %w", domain, presenceErr)
		}
		readyRows = append(refreshReady, presentRows...)
		terminalRows = absentRows
	}

	return readyRows, blockedRows, terminalRows, nil
}

// symbolRuntimePresenceGate reports the presence keyspace and per-row key
// function for the symbol→runtime shared-projection domains whose edge targets
// commit in the workload-materialization domain under a different acceptance unit
// (#2809 handles_route, #2855 runs_in). For every other domain it returns
// gated=false, so the second presence gate is skipped and the domain stays
// byte-identical to its phase-gate-only behavior.
func symbolRuntimePresenceGate(domain string) (GraphProjectionKeyspace, func(SharedProjectionIntentRow) string, bool) {
	switch domain {
	case DomainHandlesRoute:
		return GraphProjectionKeyspaceAPIEndpointRepoPath, handlesRouteEndpointPresenceKey, true
	case DomainRunsIn:
		return GraphProjectionKeyspaceRepoWorkloadPresence, runsInRepoWorkloadPresenceKey, true
	default:
		return "", nil, false
	}
}

// maxSharedIntentWaitSeconds reports the longest time any row has waited since
// it was created, used for shared-projection latency telemetry.
func maxSharedIntentWaitSeconds(now time.Time, rows []SharedProjectionIntentRow) float64 {
	var maxWait float64
	for _, row := range rows {
		if row.CreatedAt.IsZero() {
			continue
		}
		wait := now.Sub(row.CreatedAt).Seconds()
		if wait < 0 {
			wait = 0
		}
		if wait > maxWait {
			maxWait = wait
		}
	}
	return maxWait
}

// filterUpsertRows returns rows whose payload action is "upsert" or absent.
func filterUpsertRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	var result []SharedProjectionIntentRow
	for _, row := range rows {
		action, ok := row.Payload["action"]
		if ok {
			if s, isStr := action.(string); isStr && s != "upsert" {
				continue
			}
		}
		result = append(result, row)
	}
	return result
}
