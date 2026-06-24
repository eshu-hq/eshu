// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// LatestIntentsByRepoAndPartition deduplicates intents to the most recent per
// bounded acceptance key and partition, matching the Python
// _latest_intents_by_repo_and_partition function.
//
// The surviving rows are ordered refresh-first (is_refresh_intent DESC, then
// created_at ASC, intent_id ASC), the SAME primary ordering the indexed
// candidate SQL and appendUnhashedSharedCandidates emit (#3474). The dedup runs
// AFTER candidate selection but BEFORE SelectPartitionBatch truncates the ready
// set to the batch limit, so a created_at-only sort here re-buries a per-repo
// refresh intent behind older per-edge upsert rows that were enqueued first. The
// refresh then falls past the batch window and is never selected, while its
// fenced per-edge rows defer forever behind a repo-wide retract that never
// commits (#3451). Mirroring the refresh-first primary key guarantees the
// refresh leads the surviving set regardless of its created_at relative to the
// head upsert edges.
func LatestIntentsByRepoAndPartition(intents []SharedProjectionIntentRow) ([]SharedProjectionIntentRow, []string) {
	if len(intents) == 0 {
		return nil, nil
	}

	sorted := make([]SharedProjectionIntentRow, len(intents))
	copy(sorted, intents)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := isRepoRefreshRow(sorted[i]), isRepoRefreshRow(sorted[j])
		if ri != rj {
			return ri // refresh rows sort first (true > false)
		}
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		}
		return sorted[i].IntentID < sorted[j].IntentID
	})

	type repoPartitionKey struct {
		scopeID          string
		acceptanceUnitID string
		sourceRunID      string
		repositoryID     string
		partitionKey     string
	}

	latestByKey := make(map[repoPartitionKey]SharedProjectionIntentRow)
	order := make([]repoPartitionKey, 0)
	var supersededIDs []string

	for _, intent := range sorted {
		k := repoPartitionKey{
			scopeID:      intent.ScopeID,
			sourceRunID:  intent.SourceRunID,
			repositoryID: intent.RepositoryID,
			partitionKey: intent.PartitionKey,
		}
		if acceptanceKey, ok := intent.AcceptanceKey(); ok {
			k.scopeID = acceptanceKey.ScopeID
			k.acceptanceUnitID = acceptanceKey.AcceptanceUnitID
			k.sourceRunID = acceptanceKey.SourceRunID
		}
		if prev, ok := latestByKey[k]; ok {
			supersededIDs = append(supersededIDs, prev.IntentID)
		} else {
			order = append(order, k)
		}
		latestByKey[k] = intent
	}

	result := make([]SharedProjectionIntentRow, 0, len(order))
	for _, k := range order {
		result = append(result, latestByKey[k])
	}

	return result, supersededIDs
}

// FilterAuthoritativeIntents splits intents into active (matching accepted
// generation) and stale (mismatching generation) sets, matching the Python
// _filter_authoritative_intents function.
func FilterAuthoritativeIntents(
	intents []SharedProjectionIntentRow,
	acceptedGen AcceptedGenerationLookup,
) (active []SharedProjectionIntentRow, staleIDs []string) {
	for _, intent := range intents {
		key, ok := intent.AcceptanceKey()
		if !ok {
			continue
		}

		accepted, ok := acceptedGen(key)
		if !ok {
			continue
		}
		if intent.GenerationID != accepted {
			staleIDs = append(staleIDs, intent.IntentID)
			continue
		}
		active = append(active, intent)
	}
	return active, staleIDs
}
