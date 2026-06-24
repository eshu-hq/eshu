// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
)

type repositoryAccessFilter struct {
	allScopes            bool
	allowedScopeIDs      []string
	allowedRepositoryIDs []string
	allowed              map[string]struct{}
}

func repositoryAccessFilterFromContext(ctx context.Context) repositoryAccessFilter {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || auth.AllScopes || auth.Mode == AuthModeShared {
		return repositoryAccessFilter{allScopes: true}
	}
	filter := repositoryAccessFilter{
		allowedScopeIDs:      cleanedAuthStrings(auth.AllowedScopeIDs),
		allowedRepositoryIDs: cleanedAuthStrings(auth.AllowedRepositoryIDs),
		allowed:              make(map[string]struct{}, len(auth.AllowedScopeIDs)+len(auth.AllowedRepositoryIDs)),
	}
	for _, id := range filter.allowedScopeIDs {
		filter.allowed[id] = struct{}{}
	}
	for _, id := range filter.allowedRepositoryIDs {
		filter.allowed[id] = struct{}{}
	}
	return filter
}

func (f repositoryAccessFilter) scoped() bool {
	return !f.allScopes
}

func (f repositoryAccessFilter) empty() bool {
	return f.scoped() && len(f.allowed) == 0
}

func (f repositoryAccessFilter) allowsRepositoryID(repoID string) bool {
	if f.allScopes {
		return true
	}
	if repoID == "" {
		return false
	}
	_, ok := f.allowed[repoID]
	return ok
}

func (f repositoryAccessFilter) graphParams(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	if !f.scoped() {
		return params
	}
	params["allowed_repository_ids"] = append([]string(nil), f.allowedRepositoryIDs...)
	params["allowed_scope_ids"] = append([]string(nil), f.allowedScopeIDs...)
	return params
}

func (f repositoryAccessFilter) graphPredicate(alias string) string {
	if !f.scoped() {
		return ""
	}
	return " AND " + f.graphCondition(alias)
}

func (f repositoryAccessFilter) graphWhereClause(alias string) string {
	if !f.scoped() {
		return ""
	}
	return "WHERE " + f.graphCondition(alias)
}

func (f repositoryAccessFilter) graphCondition(alias string) string {
	return "(" + alias + ".id IN $allowed_repository_ids OR " + alias + ".id IN $allowed_scope_ids)"
}

func (f repositoryAccessFilter) filterCatalogEntries(entries []RepositoryCatalogEntry) []RepositoryCatalogEntry {
	if !f.scoped() {
		return entries
	}
	filtered := make([]RepositoryCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if f.allowsRepositoryID(entry.ID) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (f repositoryAccessFilter) filterRepositoryMaps(repos []map[string]any) []map[string]any {
	if !f.scoped() {
		return repos
	}
	filtered := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		if f.allowsRepositoryID(StringVal(repo, "id")) {
			filtered = append(filtered, repo)
		}
	}
	return filtered
}

func (f repositoryAccessFilter) repositorySearchIDs() []string {
	if !f.scoped() {
		return nil
	}
	ids := make([]string, 0, len(f.allowed))
	for id := range f.allowed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// grantedRepositoryIDs returns a copy of the scoped-token's granted repository
// ids (empty for shared/admin/local). Callers that bind a graph predicate on
// the `$allowed_repository_ids` parameter use this alongside grantedScopeIDs so
// the two arrays stay distinct, matching repositoryAccessFilter.graphParams.
func (f repositoryAccessFilter) grantedRepositoryIDs() []string {
	if !f.scoped() {
		return nil
	}
	return append([]string(nil), f.allowedRepositoryIDs...)
}

// grantedScopeIDs returns a copy of the scoped-token's granted ingestion-scope
// ids (empty for shared/admin/local). Pairs with grantedRepositoryIDs for graph
// predicates that bind the `$allowed_scope_ids` parameter.
func (f repositoryAccessFilter) grantedScopeIDs() []string {
	if !f.scoped() {
		return nil
	}
	return append([]string(nil), f.allowedScopeIDs...)
}
