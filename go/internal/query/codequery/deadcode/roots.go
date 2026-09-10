// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// codeRootVerdictStore reads reducer-materialized downgraded code-root verdicts
// for the active generation, keyed per repository and candidate entity.
type codeRootVerdictStore interface {
	DowngradedCodeRootKinds(ctx context.Context, repoID string, entityIDs []string) (map[string]map[string]struct{}, error)
}

// LoadDeadCodeDowngradedRoots batch-loads the downgraded verdict kinds for the
// candidate results, grouped by repository. It is FAIL-OPEN by lag-safety
// design: a store that does not implement the verdict interface, a missing
// table, a lagging or non-active-generation reducer, an empty result, or any
// store error all yield a nil/empty map, so the dead-code policy keeps exactly
// what it keeps today. This is what makes it structurally impossible for the
// feature to newly flag a candidate dead except via a positive, active-generation
// downgraded row.
func (a *Analyzer) LoadDeadCodeDowngradedRoots(ctx context.Context, results []map[string]any) codemodel.DeadCodeDowngradedRoots {
	if a == nil {
		return nil
	}
	store, ok := a.deps.Content.(codeRootVerdictStore)
	if !ok {
		return nil
	}
	byRepo := make(map[string][]string)
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		repoID := strings.TrimSpace(querycontract.StringVal(result, "repo_id"))
		entityID := strings.TrimSpace(querycontract.StringVal(result, "entity_id"))
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
	downgraded := make(codemodel.DeadCodeDowngradedRoots)
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

// This file is the codequery-local mirror of root package query's
// contract.go, ports.go, handler.go, and neo4j.go compatibility shims
// (#6060, lane A CodeHandler move). Every declaration below is a plain type
// alias or thin forwarder onto querycontract (or another dependency-neutral
// leaf package), nothing here implements new behavior. It exists so the
// moved CodeHandler-family files keep every unqualified call site they had
// in package query, unchanged: several of those call sites sit inside
// functions registered in internal/queryplan/grandfathered_non_hot.go with a
// source digest frozen from the `func` keyword through the closing brace,
