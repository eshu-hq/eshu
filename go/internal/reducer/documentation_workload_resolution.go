// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// resolveWorkloadTargets runs workload-kind targets through the handle
// ladder before the DOCUMENTS commit: a spanning handle drops its row as
// workload_handle_ambiguous rather than linking one tenant. A nil
// WorkloadLookup keeps the legacy rows. A lookup failure fails the intent
// closed for retry.
func (h DocumentationEdgeMaterializationHandler) resolveWorkloadTargets(
	ctx context.Context,
	intent Intent,
	rows []map[string]any,
) ([]map[string]any, error) {
	if h.WorkloadLookup == nil {
		return rows, nil
	}
	return ResolveDocumentationWorkloadTargets(ctx, rows, intent.ScopeID, intent.GenerationID, &handle.Resolver{
		ScopeRepos:  h.ScopeRepos,
		Lookup:      handle.SiblingLookup{Siblings: h.WorkloadLookup},
		Instruments: h.Instruments,
	})
}

// ResolveDocumentationWorkloadTargets resolves every workload-kind row's
// target_entity_id through the handle ladder before the DOCUMENTS commit,
// restricted by the scope's repositories for repository-bound scopes.
//
// A full id or unique handle rewrites the row to the resolved node id. A
// handle spanning two repositories drops the row — skipped and counted as
// workload_handle_ambiguous, never silent, never linked. A miss keeps the
// row so the writer's exact MATCH stays a no-op, exactly as today.
// Non-workload target kinds pass through untouched. A nil resolver keeps
// every row (the unwired-handler legacy path).
func ResolveDocumentationWorkloadTargets(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
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
		if !isWorkloadDocumentationTarget(row) {
			continue
		}
		items = append(items, handle.Item{Selector: anyToString(row["target_entity_id"])})
	}
	resolutions, err := resolver.ResolveAll(ctx, scopeID, generationID, items)
	if err != nil {
		return nil, err
	}
	kept := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !isWorkloadDocumentationTarget(row) {
			kept = append(kept, row)
			continue
		}
		item := handle.Item{Selector: anyToString(row["target_entity_id"])}
		switch resolutions[item.Key()].Outcome {
		case handle.OutcomeAmbiguous:
			continue
		case handle.OutcomeExact, handle.OutcomeUniqueByName:
			row["target_entity_id"] = resolutions[item.Key()].WorkloadID
		case handle.OutcomeMiss:
			// Kept below: the writer's exact MATCH stays a no-op.
		}
		kept = append(kept, row)
	}
	return kept, nil
}

func isWorkloadDocumentationTarget(row map[string]any) bool {
	return strings.TrimSpace(anyToString(row["target_kind"])) == "workload"
}
