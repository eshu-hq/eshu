// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// scopedTestFilter builds a scoped filter from slices alone, leaving the
// derived cache empty so slice-authority (not cache) decides.
func scopedTestFilter() RepositoryAccessFilter {
	return RepositoryAccessFilter{
		AllowedScopeIDs:      []string{"scope-a"},
		AllowedRepositoryIDs: []string{"repo-a"},
	}
}

// TestAllowsGrants proves canonical, direct-scope, and combined grant
// checks with the unscoped bypass.
func TestAllowsGrants(t *testing.T) {
	t.Parallel()

	open := RepositoryAccessFilter{AllScopes: true}
	if !open.AllowsCanonicalRepositoryID("anything") || !open.AllowsRepositoryID("anything") {
		t.Fatal("unscoped filter must allow every id")
	}
	if open.AllowsDirectScopeID("scope-a") {
		t.Fatal("unscoped caller must never match a direct scope grant")
	}
	scoped := scopedTestFilter()
	if !scoped.AllowsCanonicalRepositoryID("repo-a") || scoped.AllowsCanonicalRepositoryID("scope-a") {
		t.Fatal("canonical check must use repository ids only")
	}
	if !scoped.AllowsDirectScopeID("scope-a") || scoped.AllowsDirectScopeID("repo-a") {
		t.Fatal("direct-scope check must use scope ids only")
	}
	if !scoped.AllowsRepositoryID("scope-a") || !scoped.AllowsRepositoryID("repo-a") {
		t.Fatal("combined check must honor both slices")
	}
	if scoped.AllowsRepositoryID("") || scoped.AllowsRepositoryID("other") {
		t.Fatal("combined check must reject blank and foreign ids")
	}
	if !ContainsAuthString([]string{"a"}, "a") || ContainsAuthString([]string{"a"}, "") || ContainsAuthString([]string{"a"}, "b") {
		t.Fatal("ContainsAuthString misreports membership")
	}
}

// TestGraphParamHelpers proves param containment and whitespace folding.
func TestGraphParamHelpers(t *testing.T) {
	t.Parallel()

	params := map[string]any{"ids": []string{"a", "b"}}
	if !GraphParamContains(params, "ids", "a") || GraphParamContains(params, "ids", "z") {
		t.Fatal("GraphParamContains misreports membership")
	}
	if GraphParamContains(params, "missing", "a") || GraphParamContains(params, "ids", "") {
		t.Fatal("GraphParamContains must reject missing keys and blank candidates")
	}
	if GraphParamContains(map[string]any{"ids": "a"}, "ids", "a") {
		t.Fatal("GraphParamContains must reject non-slice params")
	}
	if got := NormalizeCypherWhitespace("MATCH  (n)\n\tWHERE  n.id = 1"); got != "MATCH (n) WHERE n.id = 1" {
		t.Fatalf("NormalizeCypherWhitespace = %q, want single-spaced", got)
	}
}

// TestGraphParamsAndPredicates proves scoped params bind both arrays plus
// scalar keys while unscoped callers pass through, and predicate text
// gates on Scoped.
func TestGraphParamsAndPredicates(t *testing.T) {
	t.Parallel()

	scoped := scopedTestFilter()
	params := scoped.GraphParams(nil)
	if params["allowed_repository_ids"] == nil || params["allowed_scope_ids"] == nil {
		t.Fatalf("scoped GraphParams = %#v, want both grant arrays", params)
	}
	if params["scope_grant_0"] == nil {
		t.Fatalf("scoped GraphParams = %#v, want bound scalar keys", params)
	}
	open := RepositoryAccessFilter{AllScopes: true}
	if got := open.GraphParams(nil); len(got) != 0 {
		t.Fatalf("unscoped GraphParams = %#v, want empty passthrough", got)
	}
	if got := scoped.GraphPredicate("n"); !strings.HasPrefix(got, " AND ") {
		t.Fatalf("GraphPredicate = %q, want a leading-AND condition", got)
	}
	if got := open.GraphPredicate("n"); got != "" {
		t.Fatalf("unscoped GraphPredicate = %q, want empty", got)
	}
	if got := scoped.GraphWhereClause("n"); !strings.HasPrefix(got, "WHERE ") {
		t.Fatalf("GraphWhereClause = %q, want a WHERE clause", got)
	}
	condition := scoped.GraphCondition("n")
	if !strings.Contains(condition, "n.id IN $allowed_repository_ids") {
		t.Fatalf("GraphCondition = %q, want the id grant condition", condition)
	}
	prop := scoped.GraphConditionOnProperty("w", "repo_id")
	if !strings.Contains(prop, "w.repo_id IN $allowed_scope_ids") {
		t.Fatalf("GraphConditionOnProperty = %q, want the property grant condition", prop)
	}
	if got := scoped.GraphPredicateOnProperty("w", "repo_id"); !strings.HasPrefix(got, " AND ") {
		t.Fatalf("GraphPredicateOnProperty = %q, want a leading-AND condition", got)
	}
	if got := scoped.GraphWhereClauseOnProperty("w", "repo_id"); !strings.HasPrefix(got, "\nWHERE ") {
		t.Fatalf("GraphWhereClauseOnProperty = %q, want a newline WHERE clause", got)
	}
	if got := open.GraphPredicateOnProperty("w", "repo_id"); got != "" {
		t.Fatalf("unscoped GraphPredicateOnProperty = %q, want empty", got)
	}
}

// TestFilterEntriesAndMaps proves scoped filtering admits grants only
// while unscoped callers pass everything through.
func TestFilterEntriesAndMaps(t *testing.T) {
	t.Parallel()

	entries := []RepositoryCatalogEntry{{ID: "repo-a"}, {ID: "repo-b"}}
	scoped := scopedTestFilter()
	filtered := scoped.FilterCatalogEntries(entries)
	if len(filtered) != 1 || filtered[0].ID != "repo-a" {
		t.Fatalf("FilterCatalogEntries = %#v, want only repo-a", filtered)
	}
	open := RepositoryAccessFilter{AllScopes: true}
	if got := open.FilterCatalogEntries(entries); len(got) != 2 {
		t.Fatalf("unscoped FilterCatalogEntries = %#v, want passthrough", got)
	}
	repos := []map[string]any{{"id": "repo-a"}, {"id": "repo-b"}}
	if got := scoped.FilterRepositoryMaps(repos); len(got) != 1 {
		t.Fatalf("FilterRepositoryMaps = %#v, want only repo-a", got)
	}
	if got := scoped.GrantedRepositoryIDs(); !reflect.DeepEqual(got, []string{"repo-a"}) {
		t.Fatalf("GrantedRepositoryIDs = %#v, want [repo-a]", got)
	}
	if got := scoped.GrantedScopeIDs(); !reflect.DeepEqual(got, []string{"scope-a"}) {
		t.Fatalf("GrantedScopeIDs = %#v, want [scope-a]", got)
	}
	if got := open.GrantedRepositoryIDs(); got != nil {
		t.Fatalf("unscoped GrantedRepositoryIDs = %#v, want nil", got)
	}
}

// TestScopeGrantInlineScalars proves deterministic union ordering with
// dedup, blank dropping, and the fail-closed cap.
func TestScopeGrantInlineScalars(t *testing.T) {
	t.Parallel()

	scalars, capped := ScopeGrantInlineScalars([]string{"repo-b", "repo-a", "", "repo-a"}, []string{"scope-a", "repo-b"})
	if capped || !reflect.DeepEqual(scalars, []string{"repo-a", "repo-b", "scope-a"}) {
		t.Fatalf("ScopeGrantInlineScalars = (%#v, %v), want sorted dedup without cap", scalars, capped)
	}
	many := make([]string, 0, MaxScopeGrantInlineTerms+10)
	for i := 0; i < MaxScopeGrantInlineTerms+10; i++ {
		many = append(many, "repo-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	cappedScalars, capped := ScopeGrantInlineScalars(many, nil)
	if !capped || len(cappedScalars) != MaxScopeGrantInlineTerms {
		t.Fatalf("overflow scalars len = %d capped = %v, want %d and true", len(cappedScalars), capped, MaxScopeGrantInlineTerms)
	}
	disjunction := ScopeGrantInlineMapDisjunction("n", ScopeHopInbound, "USES", "WorkloadInstance", "repo_id", []string{"a", "b"})
	if !strings.Contains(disjunction, "(n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0})") ||
		!strings.Contains(disjunction, " OR ") {
		t.Fatalf("inbound disjunction = %q, want the OR-chained pattern", disjunction)
	}
	outbound := ScopeGrantInlineMapDisjunction("n", ScopeHopOutbound, "DEFINES", "Repository", "id", []string{"a"})
	if !strings.Contains(outbound, "(n)-[:DEFINES]->(:Repository {id:$scope_grant_0})") {
		t.Fatalf("outbound disjunction = %q, want the outbound arrow", outbound)
	}
	if got := ScopeGrantInlineMapDisjunction("n", ScopeHopInbound, "USES", "X", "id", nil); got != "" {
		t.Fatalf("empty disjunction = %q, want empty", got)
	}
	params := map[string]any{}
	BindScopeGrantInlineScalars(params, []string{"a", "b"})
	if params["scope_grant_0"] != "a" || params["scope_grant_1"] != "b" {
		t.Fatalf("bound params = %#v, want scope_grant_0/1", params)
	}
	BindScopeGrantInlineScalars(params, nil)
	if len(params) != 2 {
		t.Fatalf("empty bind changed params = %#v, want no-op", params)
	}
}

// TestFilterScopeGrantScalars proves the access-filter companion derives
// the same slice with the cap signaled separately.
func TestFilterScopeGrantScalars(t *testing.T) {
	t.Parallel()

	scoped := scopedTestFilter()
	scalars, capped := scoped.ScopeGrantInlineScalars()
	if capped || !reflect.DeepEqual(scalars, []string{"repo-a", "scope-a"}) {
		t.Fatalf("filter scalars = (%#v, %v), want the union", scalars, capped)
	}
	if scoped.GrantInlineCapExceeded() {
		t.Fatal("small grant set must not exceed the cap")
	}
	open := RepositoryAccessFilter{AllScopes: true}
	if scalars, capped := open.ScopeGrantInlineScalars(); scalars != nil || capped {
		t.Fatalf("unscoped scalars = (%#v, %v), want (nil, false)", scalars, capped)
	}
	predicate := WorkloadScopePredicate("w", scoped)
	if !strings.Contains(predicate, "w.repo_id IN $allowed_repository_ids") ||
		!strings.Contains(predicate, "DEFINES") {
		t.Fatalf("WorkloadScopePredicate = %q, want ownership plus DEFINES disjuncts", predicate)
	}
	bare := WorkloadScopePredicate("w", open)
	if !strings.Contains(bare, "w.repo_id IN $allowed_repository_ids") || strings.Contains(bare, "DEFINES") {
		t.Fatalf("unscoped WorkloadScopePredicate = %q, want ownership only", bare)
	}
}

// TestRepositoryAccessFilterFromContext proves no-context and all-scopes
// stay unscoped while scoped contexts derive the grant union.
func TestRepositoryAccessFilterFromContext(t *testing.T) {
	t.Parallel()

	if got := RepositoryAccessFilterFromContext(context.Background()); !got.AllScopes {
		t.Fatalf("context-less filter = %#v, want AllScopes", got)
	}
	allScopes := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{AllScopes: true})
	if got := RepositoryAccessFilterFromContext(allScopes); !got.AllScopes {
		t.Fatalf("all-scopes filter = %#v, want AllScopes", got)
	}
	shared := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeShared})
	if got := RepositoryAccessFilterFromContext(shared); !got.AllScopes {
		t.Fatalf("shared-mode filter = %#v, want AllScopes", got)
	}
	scoped := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode: auth.AuthModeScoped, AllowedScopeIDs: []string{" scope-a ", ""}, AllowedRepositoryIDs: []string{"repo-a"},
	})
	got := RepositoryAccessFilterFromContext(scoped)
	if got.AllScopes || !got.AllowsRepositoryID("scope-a") || !got.AllowsRepositoryID("repo-a") {
		t.Fatalf("scoped filter = %#v, want the cleaned grant union", got)
	}
}
