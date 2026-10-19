// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// resolveWorkloadHandles runs the row workload_ids through the handle
// ladder before the USES commit: a spanning handle drops its row as
// workload_handle_ambiguous rather than fanning one resource onto every
// tenant's instance of a name. A nil WorkloadLookup keeps the legacy
// pass-through rows. A lookup failure fails the intent closed for retry.
func (h WorkloadCloudRelationshipMaterializationHandler) resolveWorkloadHandles(
	ctx context.Context,
	intent reducercontract.Intent,
	rows []map[string]any,
	tally *workloadCloudRelationshipTally,
) ([]map[string]any, error) {
	if h.WorkloadLookup == nil {
		return rows, nil
	}
	return ResolveWorkloadCloudRelationshipRows(ctx, rows, intent.ScopeID, intent.GenerationID, tally, &handle.Resolver{
		ScopeRepos:  h.ScopeRepos,
		Lookup:      handle.SiblingLookup{Siblings: h.WorkloadLookup},
		Instruments: h.Instruments,
	})
}

// ResolveWorkloadCloudRelationshipRows resolves every row's workload_id
// through the handle ladder before the USES commit, restricted by the
// scope's repositories (ListScopeRepositoryIDs for repository-bound scopes)
// or the row's workload_repository_id when the collector set one.
//
// A full id or unique handle rewrites the row to the resolved node id. A
// handle spanning two repositories drops the row and tallies
// workload_handle_ambiguous — never attached to one tenant, never counted
// as ambiguous_anchor. A miss keeps the row so the writer's exact MATCH
// stays a no-op, exactly as today. A nil resolver keeps every row
// (the unwired-handler legacy path).
func ResolveWorkloadCloudRelationshipRows(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	tally *workloadCloudRelationshipTally,
	resolver *handle.Resolver,
) ([]map[string]any, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	if resolver == nil || resolver.Lookup == nil {
		return rows, nil
	}
	items := make([]handle.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, handle.Item{
			Selector: anyToString(row["workload_id"]),
			RepoID:   anyToString(row["workload_repository_id"]),
		})
	}
	resolutions, err := resolver.ResolveAll(ctx, scopeID, generationID, items)
	if err != nil {
		return nil, err
	}
	kept := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := handle.Item{
			Selector: anyToString(row["workload_id"]),
			RepoID:   anyToString(row["workload_repository_id"]),
		}
		resolution := resolutions[item.Key()]
		switch resolution.Outcome {
		case handle.OutcomeAmbiguous:
			tally.skipped[handle.SkipReasonHandleAmbiguous]++
			continue
		case handle.OutcomeExact, handle.OutcomeUniqueByName:
			row["workload_id"] = resolution.WorkloadID
		case handle.OutcomeMiss:
			// Kept below: the writer's exact MATCH stays a no-op.
		}
		kept = append(kept, row)
	}
	return kept, nil
}
