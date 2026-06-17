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

	// Second gate (handles_route only, #2809): among the phase-ready rows,
	// terminally drain the ones whose (repo_id, path) :Endpoint did not commit.
	// Because the phase gate already proves workload materialization for the repo
	// is done, an absent endpoint will never appear, so the row is complete with
	// no edge — NOT deferred. A nil presence lookup is a no-op, so every other
	// domain — and handles_route when presence is unwired — is unaffected.
	if domain == DomainHandlesRoute && endpointPresence != nil && len(readyRows) > 0 {
		presentRows, absentRows, presenceErr := filterHandlesRouteRowsByEndpointPresence(ctx, readyRows, endpointPresence)
		if presenceErr != nil {
			return nil, nil, nil, fmt.Errorf("look up handles_route endpoint presence: %w", presenceErr)
		}
		readyRows = presentRows
		terminalRows = absentRows
	}

	return readyRows, blockedRows, terminalRows, nil
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
