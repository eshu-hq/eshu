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
func filterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
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
