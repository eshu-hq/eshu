package reducer

import (
	"context"
	"fmt"
	"time"
)

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
			key, ok := graphProjectionPhaseKeyForIntent(row, row.GenerationID, keyspace)
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
		key, ok := graphProjectionPhaseKeyForIntent(row, row.GenerationID, keyspace)
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
		presentRows, absentRows, presenceErr := filterRowsByTargetPresence(ctx, readyRows, endpointPresence, keyspace, keyFor)
		if presenceErr != nil {
			return nil, nil, nil, fmt.Errorf("look up %s target presence: %w", domain, presenceErr)
		}
		readyRows = presentRows
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
