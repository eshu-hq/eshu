package reducer

import (
	"context"
	"fmt"
	"time"
)

// filterRowsByReadiness partitions a domain's pending intent rows into the rows
// whose prerequisite graph-projection phase has committed and the rows still
// blocked on readiness. Domains without a readiness gate pass through
// unchanged. The readiness key is built under the domain's prerequisite
// keyspace (sharedProjectionReadinessKeyspace) so a multi-keyspace domain such
// as handles_route looks up the phase under the keyspace it was published in.
//
// endpointPresence adds a SECOND, domain-scoped gate that applies ONLY to
// DomainHandlesRoute (#2809): a handles_route intent carries the repo acceptance
// unit, but its target :Endpoint commits under a per-workload acceptance unit, so
// the phase gate alone cannot prove the endpoint exists. After the phase gate,
// handles_route rows are additionally filtered by property-keyed (repo_id, path)
// endpoint presence; rows whose endpoint is absent join the blocked set and are
// deferred (re-enqueued, not marked complete). A nil endpointPresence disables
// this second gate, so every other domain — and handles_route itself when
// presence is unwired — stays byte-identical to its pre-#2809 behavior.
func filterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
) ([]SharedProjectionIntentRow, []SharedProjectionIntentRow, error) {
	phase, gated := sharedProjectionReadinessPhase(domain)
	if !gated || len(rows) == 0 {
		return rows, nil, nil
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
		resolvedLookup, err := readinessPrefetch(ctx, keys, phase)
		if err != nil {
			return nil, nil, fmt.Errorf("prefetch graph projection readiness: %w", err)
		}
		lookup = resolvedLookup
	}

	if lookup == nil {
		return rows, nil, nil
	}

	readyRows := make([]SharedProjectionIntentRow, 0, len(rows))
	blockedRows := make([]SharedProjectionIntentRow, 0)
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

	// Second gate (handles_route only, #2809): among the phase-ready rows, defer
	// the ones whose (repo_id, path) :Endpoint has not committed yet. A nil
	// presence lookup is a no-op, so every other domain — and handles_route when
	// presence is unwired — is unaffected.
	if domain == DomainHandlesRoute && endpointPresence != nil && len(readyRows) > 0 {
		presentRows, absentRows, err := filterHandlesRouteRowsByEndpointPresence(ctx, readyRows, endpointPresence)
		if err != nil {
			return nil, nil, fmt.Errorf("look up handles_route endpoint presence: %w", err)
		}
		readyRows = presentRows
		blockedRows = append(blockedRows, absentRows...)
	}

	return readyRows, blockedRows, nil
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
