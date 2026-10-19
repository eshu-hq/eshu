// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// fetchWorkload queries Neo4j for the workload by ID.
func (h *Handler) fetchWorkload(ctx context.Context, workloadID string) (map[string]any, error) {
	cypher := `
		MATCH (w:Workload) WHERE w.id = $workload_id
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
	`
	row, err := h.Neo4j.RunSingle(ctx, cypher, map[string]any{"workload_id": workloadID})
	if err != nil {
		return nil, err
	}
	if row != nil {
		if id := compareStringVal(row, "id"); id == "" || id == workloadID {
			return workloadRowMap(row), nil
		}
		// An id-lookup row carrying a different id is backend drift, not
		// the requested workload; fall through to the name rung only for
		// handles, otherwise report missing like before.
		if workloadid.ClassifyWorkloadSelector(workloadID) != workloadid.SelectorKindHandle {
			return nil, nil
		}
	} else if workloadid.ClassifyWorkloadSelector(workloadID) != workloadid.SelectorKindHandle {
		return nil, nil
	}
	// #5385 flip wave, ladder rung 2: a workload: handle matches no
	// post-flip node id, so resolve the stripped bare name through a bounded
	// candidate read. Names are not unique across repositories, so the read
	// pages bound+1 rows ordered by id and the lowest id whose repository
	// the caller may see wins deterministically; anything else (no
	// candidate, no granted candidate, a page over the bound) reads as
	// missing, identically to an ungranted id hit, so the route stays free
	// of existence oracles for other tenants' workloads. The grant check on
	// the resolved repo_id still runs at the caller.
	return h.fetchWorkloadByName(ctx, workloadid.StripWorkloadHandlePrefix(workloadID))
}

// fetchWorkloadByName resolves the stripped handle name to the lowest
// workload id the caller may see. It returns (nil, nil) when no candidate or
// no granted candidate fits in the bound.
func (h *Handler) fetchWorkloadByName(ctx context.Context, name string) (map[string]any, error) {
	if name == "" {
		return nil, nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (w:Workload) WHERE w.name = $workload_name
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
		ORDER BY w.id
		LIMIT $limit
	`, map[string]any{"workload_name": name, "limit": querycontract.WorkloadSelectorCandidateBound + 1})
	if err != nil {
		return nil, err
	}
	if len(rows) > querycontract.WorkloadSelectorCandidateBound {
		return nil, nil
	}
	for _, row := range rows {
		if compareStringVal(row, "name") != name || compareStringVal(row, "id") == "" {
			continue
		}
		if !impact.RepoIDAllowed(compareStringVal(row, "repo_id"), access) {
			continue
		}
		return workloadRowMap(row), nil
	}
	return nil, nil
}

// workloadRowMap projects a workload graph row onto the fetchWorkload shape.
func workloadRowMap(row map[string]any) map[string]any {
	return map[string]any{
		"id":      compareStringVal(row, "id"),
		"name":    compareStringVal(row, "name"),
		"kind":    compareStringVal(row, "kind"),
		"repo_id": compareStringVal(row, "repo_id"),
	}
}
