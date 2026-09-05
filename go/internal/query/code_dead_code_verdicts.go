// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// The rubyRailsControllerActionRootKind constant split to
// codemodel/code_dead_code_ruby_roots.go and the deadCodeDowngradedRoots
// type with its isDowngraded predicate split to
// codemodel/code_dead_code_analysis.go (#6060 lane A L1); both run in the
// leaf now. Root's family_code_shim.go aliases them back so the staying
// verdict loader, readers, and tests keep their names.

// codeRootVerdictStore reads reducer-materialized downgraded code-root verdicts
// for the active generation, keyed per repository and candidate entity.
type codeRootVerdictStore interface {
	DowngradedCodeRootKinds(ctx context.Context, repoID string, entityIDs []string) (map[string]map[string]struct{}, error)
}

// loadDeadCodeDowngradedRoots batch-loads the downgraded verdict kinds for the
// candidate results, grouped by repository. It is FAIL-OPEN by lag-safety
// design: a store that does not implement the verdict interface, a missing
// table, a lagging or non-active-generation reducer, an empty result, or any
// store error all yield a nil/empty map, so the dead-code policy keeps exactly
// what it keeps today. This is what makes it structurally impossible for the
// feature to newly flag a candidate dead except via a positive, active-generation
// downgraded row.
func (h *CodeHandler) loadDeadCodeDowngradedRoots(ctx context.Context, results []map[string]any) deadCodeDowngradedRoots {
	if h == nil {
		return nil
	}
	store, ok := h.Content.(codeRootVerdictStore)
	if !ok {
		return nil
	}
	byRepo := make(map[string][]string)
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		repoID := strings.TrimSpace(StringVal(result, "repo_id"))
		entityID := strings.TrimSpace(StringVal(result, "entity_id"))
		if repoID == "" || entityID == "" {
			continue
		}
		key := repoID + "\x00" + entityID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		byRepo[repoID] = append(byRepo[repoID], entityID)
	}
	if len(byRepo) == 0 {
		return nil
	}
	downgraded := make(deadCodeDowngradedRoots)
	for repoID, entityIDs := range byRepo {
		kindsByEntity, err := store.DowngradedCodeRootKinds(ctx, repoID, entityIDs)
		if err != nil {
			// Lag-safety fail-open: a verdict-store problem must never make the
			// dead-code query newly flag code dead (or fail the request).
			// Degrade to the pre-#5376 behavior of keeping every parser root.
			continue
		}
		for entityID, kinds := range kindsByEntity {
			if len(kinds) == 0 {
				continue
			}
			downgraded[entityID] = kinds
		}
	}
	if len(downgraded) == 0 {
		return nil
	}
	return downgraded
}
