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

func (f repositoryAccessFilter) allowsCanonicalRepositoryID(repoID string) bool {
	if f.allScopes {
		return true
	}
	return containsAuthString(f.allowedRepositoryIDs, repoID)
}

func (f repositoryAccessFilter) allowsDirectScopeID(scopeID string) bool {
	if f.allScopes {
		return false
	}
	return containsAuthString(f.allowedScopeIDs, scopeID)
}

func containsAuthString(values []string, candidate string) bool {
	if candidate == "" {
		return false
	}
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
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
	// Bind the per-grant SHAPE-A inline-map scalars (scope_grant_0..N) referenced
	// by infraResourceScopePredicate / workloadScopePredicate. Binding here keeps
	// the scoped scope-predicate consumers' query-owner source unchanged and
	// guarantees the params and predicate derive the same deterministically
	// ordered, capped slice. Queries that bind graphParams but never render an
	// inline-map disjunct simply carry unused scalar params, which the backend
	// ignores.
	scalars, _ := scopeGrantInlineScalars(f.allowedRepositoryIDs, f.allowedScopeIDs)
	bindScopeGrantInlineScalars(params, scalars)
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
	return f.graphConditionOnProperty(alias, "id")
}

// graphConditionOnProperty binds an arbitrary node property (not only the
// node's own id) to the caller's grant, for graph nodes whose grant key is a
// repository reference held on a different property -- e.g. a Workload,
// WorkloadInstance, CloudResource, TerraformModule, or DataAsset binds to the
// grant through its repo_id, while a Repository binds through its own id. It
// returns the raw condition text unconditionally; callers gate on scoped() (via
// graphPredicateOnProperty) and pass graphParams so $allowed_repository_ids /
// $allowed_scope_ids are bound.
func (f repositoryAccessFilter) graphConditionOnProperty(alias, property string) string {
	return "(" + alias + "." + property + " IN $allowed_repository_ids OR " + alias + "." + property + " IN $allowed_scope_ids)"
}

// graphPredicateOnProperty returns a leading-AND grant predicate on the given
// node property when the caller is scoped, and "" otherwise. It lets a resolver
// query push the caller's grant into its WHERE (before the LIMIT) so the LIMIT
// applies to the granted set, rather than filtering after the query where a
// cross-tenant-polluted page can drop authorized rows (#5167 W3 P1
// filter-before-limit).
func (f repositoryAccessFilter) graphPredicateOnProperty(alias, property string) string {
	if !f.scoped() {
		return ""
	}
	return " AND " + f.graphConditionOnProperty(alias, property)
}

// graphWhereClauseOnProperty returns a leading "\nWHERE <grant condition>" on
// the given node property when the caller is scoped, and "" otherwise. Used to
// push the grant onto an affected/matched repository node that has no existing
// WHERE of its own, before a query's LIMIT (#5167 W3 P1 filter-before-limit).
func (f repositoryAccessFilter) graphWhereClauseOnProperty(alias, property string) string {
	if !f.scoped() {
		return ""
	}
	return "\nWHERE " + f.graphConditionOnProperty(alias, property)
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
