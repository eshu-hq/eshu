// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"sort"
	"strings"
)

// RepositoryAccessFilter is the scoped-token authorization seam every
// repository-shaped read path filters through. It carries the caller's
// granted repository and ingestion-scope ids (empty/unscoped for shared,
// admin, and local callers, which set AllScopes) and the deduplicated
// Allowed lookup set built from both id lists. Family packages construct one
// directly with these exported fields; the moved-out root package builds it
// from the request's AuthContext (repositoryAccessFilterFromContext) and
// forwards call sites through the type alias so a caller can keep naming it
// without importing this package.
type RepositoryAccessFilter struct {
	AllScopes            bool
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
	Allowed              map[string]struct{}
}

// Scoped reports whether this filter restricts access to a granted subset,
// as opposed to an unscoped (shared/admin/local) caller that sees everything.
func (f RepositoryAccessFilter) Scoped() bool {
	return !f.AllScopes
}

// Empty reports whether a scoped caller has no grants at all, so every
// scoped read should fail closed to zero rows rather than fall through to an
// unscoped query shape.
//
// The exported id slices are authoritative and Allowed is a derived lookup
// cache. Consulting only the cache would report a filter built as
// RepositoryAccessFilter{AllowedRepositoryIDs: ...} as ungranted, and silently
// drop that caller's valid scoped reads.
func (f RepositoryAccessFilter) Empty() bool {
	if !f.Scoped() {
		return false
	}
	return len(f.Allowed) == 0 &&
		len(f.AllowedScopeIDs) == 0 &&
		len(f.AllowedRepositoryIDs) == 0
}

// AllowsRepositoryID reports whether repoID is in the caller's combined
// repository/scope grant set. Unscoped callers allow every id.
//
// Allowed is a derived lookup cache over the two exported id slices, which are
// authoritative. A filter constructed from the slices alone carries real grants
// and must be honoured; consulting only the cache would deny an authorized
// repository.
func (f RepositoryAccessFilter) AllowsRepositoryID(repoID string) bool {
	if f.AllScopes {
		return true
	}
	if repoID == "" {
		return false
	}
	if _, ok := f.Allowed[repoID]; ok {
		return true
	}
	return ContainsAuthString(f.AllowedRepositoryIDs, repoID) ||
		ContainsAuthString(f.AllowedScopeIDs, repoID)
}

// AllowsCanonicalRepositoryID reports whether repoID is in the caller's
// granted repository ids specifically (not the combined scope+repository
// set). Unscoped callers allow every id.
func (f RepositoryAccessFilter) AllowsCanonicalRepositoryID(repoID string) bool {
	if f.AllScopes {
		return true
	}
	return ContainsAuthString(f.AllowedRepositoryIDs, repoID)
}

// AllowsDirectScopeID reports whether scopeID is one of the caller's granted
// ingestion-scope ids specifically. Unscoped callers never match here — an
// unscoped caller is authorized through AllScopes, not a direct scope grant.
func (f RepositoryAccessFilter) AllowsDirectScopeID(scopeID string) bool {
	if f.AllScopes {
		return false
	}
	return ContainsAuthString(f.AllowedScopeIDs, scopeID)
}

// ContainsAuthString reports whether candidate is present in values. It
// treats an empty candidate as never present. Exported so root-package call
// sites unrelated to RepositoryAccessFilter (e.g. runtime-context grant
// checks) can reuse it through the containsAuthString forwarder.
func ContainsAuthString(values []string, candidate string) bool {
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

// GraphParams merges the caller's grant arrays (and, for a scoped caller,
// the SHAPE-A inline-map scalar params) into params, creating params when
// nil. Unscoped callers return params unchanged.
func (f RepositoryAccessFilter) GraphParams(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	if !f.Scoped() {
		return params
	}
	params["allowed_repository_ids"] = append([]string(nil), f.AllowedRepositoryIDs...)
	params["allowed_scope_ids"] = append([]string(nil), f.AllowedScopeIDs...)
	// Bind the per-grant SHAPE-A inline-map scalars (scope_grant_0..N) referenced
	// by infraResourceScopePredicate / workloadScopePredicate. Binding here keeps
	// the scoped scope-predicate consumers' query-owner source unchanged and
	// guarantees the params and predicate derive the same deterministically
	// ordered, capped slice. Queries that bind graphParams but never render an
	// inline-map disjunct simply carry unused scalar params, which the backend
	// ignores.
	scalars, _ := ScopeGrantInlineScalars(f.AllowedRepositoryIDs, f.AllowedScopeIDs)
	BindScopeGrantInlineScalars(params, scalars)
	return params
}

// GraphPredicate returns a leading-AND grant predicate on the alias's `id`
// property when the caller is scoped, and "" otherwise.
func (f RepositoryAccessFilter) GraphPredicate(alias string) string {
	if !f.Scoped() {
		return ""
	}
	return " AND " + f.GraphCondition(alias)
}

// GraphWhereClause returns a "WHERE <grant condition>" clause on the
// alias's `id` property when the caller is scoped, and "" otherwise.
func (f RepositoryAccessFilter) GraphWhereClause(alias string) string {
	if !f.Scoped() {
		return ""
	}
	return "WHERE " + f.GraphCondition(alias)
}

// GraphCondition returns the raw grant condition text for alias's `id`
// property, unconditionally (callers gate on Scoped()).
func (f RepositoryAccessFilter) GraphCondition(alias string) string {
	return f.GraphConditionOnProperty(alias, "id")
}

// GraphConditionOnProperty binds an arbitrary node property (not only the
// node's own id) to the caller's grant, for graph nodes whose grant key is a
// repository reference held on a different property -- e.g. a Workload,
// WorkloadInstance, CloudResource, TerraformModule, or DataAsset binds to the
// grant through its repo_id, while a Repository binds through its own id. It
// returns the raw condition text unconditionally; callers gate on Scoped()
// (via GraphPredicateOnProperty) and pass GraphParams so
// $allowed_repository_ids / $allowed_scope_ids are bound.
func (f RepositoryAccessFilter) GraphConditionOnProperty(alias, property string) string {
	return "(" + alias + "." + property + " IN $allowed_repository_ids OR " + alias + "." + property + " IN $allowed_scope_ids)"
}

// GraphPredicateOnProperty returns a leading-AND grant predicate on the given
// node property when the caller is scoped, and "" otherwise. It lets a resolver
// query push the caller's grant into its WHERE (before the LIMIT) so the LIMIT
// applies to the granted set, rather than filtering after the query where a
// cross-tenant-polluted page can drop authorized rows (#5167 W3 P1
// filter-before-limit).
func (f RepositoryAccessFilter) GraphPredicateOnProperty(alias, property string) string {
	if !f.Scoped() {
		return ""
	}
	return " AND " + f.GraphConditionOnProperty(alias, property)
}

// GraphWhereClauseOnProperty returns a leading "\nWHERE <grant condition>" on
// the given node property when the caller is scoped, and "" otherwise. Used to
// push the grant onto an affected/matched repository node that has no existing
// WHERE of its own, before a query's LIMIT (#5167 W3 P1 filter-before-limit).
func (f RepositoryAccessFilter) GraphWhereClauseOnProperty(alias, property string) string {
	if !f.Scoped() {
		return ""
	}
	return "\nWHERE " + f.GraphConditionOnProperty(alias, property)
}

// FilterCatalogEntries returns the subset of entries the caller's grant
// allows, by id. Unscoped callers get entries back unchanged.
func (f RepositoryAccessFilter) FilterCatalogEntries(entries []RepositoryCatalogEntry) []RepositoryCatalogEntry {
	if !f.Scoped() {
		return entries
	}
	filtered := make([]RepositoryCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if f.AllowsRepositoryID(entry.ID) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// FilterRepositoryMaps returns the subset of repos (row maps carrying an
// "id" field) the caller's grant allows. Unscoped callers get repos back
// unchanged.
func (f RepositoryAccessFilter) FilterRepositoryMaps(repos []map[string]any) []map[string]any {
	if !f.Scoped() {
		return repos
	}
	filtered := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		if f.AllowsRepositoryID(StringVal(repo, "id")) {
			filtered = append(filtered, repo)
		}
	}
	return filtered
}

// GitRepositoryScopePrefix is what the git collector puts in front of a
// canonical repository id when it names that repository's ingestion scope
// (gitrepo's IngestionScope.ScopeID, whose PartitionKey and source_key are the
// bare repository id). It is the one ingestion-scope shape that names a
// repository, and stripping it is how a scope grant is read back as the
// repository it owns.
const GitRepositoryScopePrefix = "git-repository-scope:"

// CanonicalRepositoryIDForScopeID returns the canonical repository id a granted
// git repository ingestion scope names, or "" for a scope that names something
// else.
//
// The two id forms are not interchangeable at a read's predicate: repository
// rows carry the canonical id in repo_id and Repository nodes carry it in id,
// so a scope id compared against either matches nothing. #5052 is that defect
// in keyword search, where a scope-addressed request returned an empty result
// set beside a healthy indexed_document_count.
//
// The mapping is lexical because the collector builds the scope id that way,
// and payloadcore, the reducer's supply-chain filter, and the runtime-context
// store all decode it the same way in the other direction. A repository-ref
// scope ("...@<ref>") names one ref rather than the repository, and the rows a
// read would return carry no ref to check it against, so it resolves to ""
// rather than widening the grant to every ref of that repository.
func CanonicalRepositoryIDForScopeID(scopeID string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(scopeID), GitRepositoryScopePrefix)
	if !ok {
		return ""
	}
	if rest = strings.TrimSpace(rest); rest == "" || strings.Contains(rest, "@") {
		return ""
	}
	return rest
}

// WithCanonicalScopeRepositories returns a copy of f whose repository grant also
// carries the canonical repository id each granted git repository scope names,
// so a token granted only an ingestion scope reads the repository that scope
// owns instead of an empty page.
//
// It only ever adds. Resolving in place -- replacing the scope ids with what
// they decode to -- would empty the id list for a caller whose grants are all
// non-repository scopes, and an empty list is what the content builders read as
// "no repository restriction" (see codeContentGrantScope). Growing the list
// cannot reach that state, and an id that resolves to nothing is inert in every
// predicate that binds it.
//
// Unscoped callers, and scoped callers with no scope grants, are returned
// unchanged.
func (f RepositoryAccessFilter) WithCanonicalScopeRepositories() RepositoryAccessFilter {
	if !f.Scoped() || len(f.AllowedScopeIDs) == 0 {
		return f
	}
	resolved := make([]string, 0, len(f.AllowedScopeIDs))
	seen := make(map[string]struct{}, len(f.AllowedScopeIDs))
	for _, scopeID := range f.AllowedScopeIDs {
		repoID := CanonicalRepositoryIDForScopeID(scopeID)
		if repoID == "" || ContainsAuthString(f.AllowedRepositoryIDs, repoID) {
			continue
		}
		if _, dup := seen[repoID]; dup {
			continue
		}
		seen[repoID] = struct{}{}
		resolved = append(resolved, repoID)
	}
	if len(resolved) == 0 {
		return f
	}
	widened := f
	widened.AllowedRepositoryIDs = append(append([]string(nil), f.AllowedRepositoryIDs...), resolved...)
	widened.AllowedScopeIDs = append([]string(nil), f.AllowedScopeIDs...)
	widened.Allowed = make(map[string]struct{}, len(f.Allowed)+len(resolved))
	for id := range f.Allowed {
		widened.Allowed[id] = struct{}{}
	}
	for _, id := range resolved {
		widened.Allowed[id] = struct{}{}
	}
	return widened
}

// RepositorySearchIDs returns the caller's combined grant set as a sorted
// slice, for callers that need a deterministic id list rather than the
// lookup map. Unscoped callers get nil.
func (f RepositoryAccessFilter) RepositorySearchIDs() []string {
	if !f.Scoped() {
		return nil
	}
	// Same rule as Empty and AllowsRepositoryID: the exported slices are
	// authoritative and Allowed is a derived cache. Reading only the cache
	// returned an empty id list for a filter built from the slices, which
	// narrows a scoped search to nothing instead of to the caller's grants.
	capacity := len(f.Allowed) + len(f.AllowedScopeIDs) + len(f.AllowedRepositoryIDs)
	seen := make(map[string]struct{}, capacity)
	ids := make([]string, 0, capacity)
	add := func(id string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range f.Allowed {
		add(id)
	}
	for _, id := range f.AllowedScopeIDs {
		add(id)
	}
	for _, id := range f.AllowedRepositoryIDs {
		add(id)
	}
	sort.Strings(ids)
	return ids
}

// GrantedRepositoryIDs returns a copy of the scoped-token's granted repository
// ids (empty for shared/admin/local). Callers that bind a graph predicate on
// the `$allowed_repository_ids` parameter use this alongside GrantedScopeIDs so
// the two arrays stay distinct, matching RepositoryAccessFilter.GraphParams.
func (f RepositoryAccessFilter) GrantedRepositoryIDs() []string {
	if !f.Scoped() {
		return nil
	}
	return append([]string(nil), f.AllowedRepositoryIDs...)
}

// GrantedScopeIDs returns a copy of the scoped-token's granted ingestion-scope
// ids (empty for shared/admin/local). Pairs with GrantedRepositoryIDs for graph
// predicates that bind the `$allowed_scope_ids` parameter.
func (f RepositoryAccessFilter) GrantedScopeIDs() []string {
	if !f.Scoped() {
		return nil
	}
	return append([]string(nil), f.AllowedScopeIDs...)
}
