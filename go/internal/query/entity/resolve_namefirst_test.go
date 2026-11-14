// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	testcontent "github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// nameFirstContentStore answers exact entity-name searches from a fixture
// slice and records the search so tests can assert the all-types contract.
type nameFirstContentStore struct {
	testcontent.FakePortContentStore
	rows     []querycontract.EntityContent
	searches []entitycontract.EntityNameSearch
}

func (s *nameFirstContentStore) SearchEntityNames(_ context.Context, search entitycontract.EntityNameSearch) ([]querycontract.EntityContent, error) {
	s.searches = append(s.searches, search)
	rows := make([]querycontract.EntityContent, 0, len(s.rows))
	for _, row := range s.rows {
		if search.Name != "" && row.EntityName != search.Name {
			continue
		}
		rows = append(rows, row)
		if search.Limit > 0 && len(rows) >= search.Limit {
			break
		}
	}
	return rows, nil
}

// nameFirstGraph answers each fan-out branch from fixtures. Marker matching
// follows the branch query builders in resolve_namefirst.go.
type nameFirstGraph struct {
	repoRows     []map[string]any
	workloadRows []map[string]any
	packageRows  []map[string]any
	seen         []string
}

func (f *nameFirstGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	f.seen = append(f.seen, cypher)
	switch {
	case strings.Contains(cypher, "MATCH (p:Package)"):
		return append([]map[string]any(nil), f.packageRows...), nil
	case strings.Contains(cypher, "MATCH (repo:Repository) WHERE repo.id IN $repo_ids"):
		return []map[string]any{{"repo_id": "repo-team-a", "repo_name": "payments"}}, nil
	case strings.Contains(cypher, "MATCH (r:Repository)"):
		return append([]map[string]any(nil), f.repoRows...), nil
	case strings.Contains(cypher, "MATCH (w:Workload)"):
		return append([]map[string]any(nil), f.workloadRows...), nil
	default:
		return nil, errNameFirstUnexpectedQuery
	}
}

func (f *nameFirstGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := f.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

type nameFirstGraphError string

func (e nameFirstGraphError) Error() string { return string(e) }

const errNameFirstUnexpectedQuery = nameFirstGraphError("unexpected name-first graph query")

func nameFirstHandler(f *nameFirstGraph, store *nameFirstContentStore) *Handler {
	return &Handler{
		Neo4j:   f,
		Content: store,
		Profile: querycontract.ProfileLocalAuthoritative,
	}
}

func allScopesContext(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared})
}

func decodeNameFirstEntities(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	body := testutil.DecodeResponseBody(t, rec)
	entities, ok := body["entities"].([]any)
	if !ok {
		t.Fatalf("entities = %#v, want a list", body["entities"])
	}
	return entities
}

func entityKind(row map[string]any) string {
	kind, _ := row["kind"].(string)
	return kind
}

func doNameFirstResolve(handler *Handler, body string, ctx context.Context) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(body))
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	handler.ResolveEntity(rec, req)
	return rec
}

// TestResolveEntityNameFirstListsRepositoryAndWorkloadRanked is the #10659
// acceptance test: one name present as both a repository and a workload
// lists both, ranked repository-first, each with kind and next_selector, and
// the legacy workload twin carries legacy_id.
func TestResolveEntityNameFirstListsRepositoryAndWorkloadRanked(t *testing.T) {
	t.Parallel()
	fake := &nameFirstGraph{
		repoRows: []map[string]any{
			{"id": "repository:repo-payments", "name": "payments"},
		},
		workloadRows: []map[string]any{
			{"id": "repository:repo-team-a:workload:payments", "labels": []any{"Workload"}, "name": "payments", "repo_id": "repo-team-a"},
			{"id": "workload:payments", "labels": []any{"Workload"}, "name": "payments"},
		},
	}
	store := &nameFirstContentStore{}
	handler := nameFirstHandler(fake, store)
	rec := doNameFirstResolve(handler, `{"name":"payments"}`, allScopesContext(context.Background()))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	entities := decodeNameFirstEntities(t, rec)
	if len(entities) != 3 {
		t.Fatalf("entities = %#v, want repository plus two workloads", entities)
	}
	first := entities[0].(map[string]any)
	if got := entityKind(first); got != "repository" {
		t.Fatalf("first kind = %q, want repository (ranked first)", got)
	}
	second := entities[1].(map[string]any)
	if got := entityKind(second); got != "workload" {
		t.Fatalf("second kind = %q, want workload", got)
	}
	for i, raw := range entities {
		row := raw.(map[string]any)
		next, ok := row["next_selector"].(map[string]any)
		if !ok || next["argument"] == "" || next["value"] == "" {
			t.Fatalf("entities[%d] next_selector = %#v, want argument and value", i, row["next_selector"])
		}
	}
	var legacySeen, scopedSeen bool
	for _, raw := range entities {
		row := raw.(map[string]any)
		if entityKind(row) != "workload" {
			continue
		}
		if row["id"] == "workload:payments" {
			legacySeen = true
			if got := row["legacy_id"]; got != true {
				t.Fatalf("legacy twin legacy_id = %#v, want true", got)
			}
		}
		if row["id"] == "repository:repo-team-a:workload:payments" {
			scopedSeen = true
			if _, present := row["legacy_id"]; present {
				t.Fatalf("scoped workload carries legacy_id, want the key absent")
			}
		}
	}
	if !legacySeen || !scopedSeen {
		t.Fatalf("legacy twin seen=%t scoped seen=%t, want both", legacySeen, scopedSeen)
	}
}

// TestResolveEntityNameFirstCollisionListsEveryKind asserts a cross-type
// collision surfaces every kind in one ranked list instead of picking one.
func TestResolveEntityNameFirstCollisionListsEveryKind(t *testing.T) {
	t.Parallel()
	fake := &nameFirstGraph{
		repoRows: []map[string]any{
			{"id": "repository:repo-billing", "name": "billing"},
		},
		packageRows: []map[string]any{
			{"package_id": "pkg:npm:billing", "ecosystem": "npm", "normalized_name": "billing", "visibility": "public"},
		},
	}
	store := &nameFirstContentStore{rows: []querycontract.EntityContent{{
		EntityID: "content-entity:billing-fn", EntityName: "billing",
		EntityType: "Function", RepoID: "repo-team-a", RelativePath: "billing.ts",
	}}}
	handler := nameFirstHandler(fake, store)
	rec := doNameFirstResolve(handler, `{"name":"billing"}`, allScopesContext(context.Background()))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	entities := decodeNameFirstEntities(t, rec)
	kinds := map[string]bool{}
	for _, raw := range entities {
		row := raw.(map[string]any)
		kinds[entityKind(row)] = true
		if _, ok := row["next_selector"].(map[string]any); !ok {
			t.Fatalf("row %#v has no next_selector", row)
		}
	}
	for _, want := range []string{"repository", "package", "entity"} {
		if !kinds[want] {
			t.Fatalf("kinds = %v, want %q listed (no silent pick)", kinds, want)
		}
	}
	if len(store.searches) != 1 {
		t.Fatalf("content searches = %d, want exactly one all-types probe", len(store.searches))
	}
	if got := store.searches[0].EntityType; got != "" {
		t.Fatalf("content search entity type = %q, want empty (all types)", got)
	}
}

// TestResolveEntityNameFirstRejectsShortName asserts a sub-3-character
// name-first lookup is refused instead of fanning out.
func TestResolveEntityNameFirstRejectsShortName(t *testing.T) {
	t.Parallel()
	fake := &nameFirstGraph{}
	store := &nameFirstContentStore{}
	handler := nameFirstHandler(fake, store)
	rec := doNameFirstResolve(handler, `{"name":"go"}`, allScopesContext(context.Background()))
	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if len(fake.seen) != 0 || len(store.searches) != 0 {
		t.Fatalf("short name fanned out: %d graph queries, %d content searches", len(fake.seen), len(store.searches))
	}
}

// TestResolveEntityNameFirstEmptyAccessAnswersEmpty asserts a grantless
// caller gets the existing empty answer, not a fan-out.
func TestResolveEntityNameFirstEmptyAccessAnswersEmpty(t *testing.T) {
	t.Parallel()
	fake := &nameFirstGraph{
		repoRows: []map[string]any{{"id": "repository:repo-x", "name": "billing"}},
	}
	store := &nameFirstContentStore{}
	handler := nameFirstHandler(fake, store)
	grantless := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
	rec := doNameFirstResolve(handler, `{"name":"billing"}`, grantless)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if entities := decodeNameFirstEntities(t, rec); len(entities) != 0 {
		t.Fatalf("entities = %#v, want empty for a grantless caller", entities)
	}
	if len(fake.seen) != 0 {
		t.Fatalf("grantless caller reached the graph: %v", fake.seen)
	}
}

// TestBuildResolveNameFirstBranchQueriesAreBounded pins the fan-out queries:
// exact-equality predicates, grant scoping before LIMIT, deterministic order.
func TestBuildResolveNameFirstBranchQueriesAreBounded(t *testing.T) {
	t.Parallel()
	scoped := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-a"},
		Allowed:              map[string]struct{}{"repo-a": {}},
	}
	cypher, params := BuildResolveNameFirstRepositoryQuery("billing", 10, scoped)
	for _, want := range []string{"MATCH (r:Repository)", "r.name = $name", "r.repo_slug = $name", "allowed_repository_ids", "ORDER BY r.id", "LIMIT $limit"} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("repository query missing %q:\n%s", want, cypher)
		}
	}
	if got, want := params["limit"], 11; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
	unscopedCypher, _ := BuildResolveNameFirstRepositoryQuery("billing", 10, querycontract.RepositoryAccessFilter{AllScopes: true})
	if strings.Contains(unscopedCypher, "allowed_repository_ids") {
		t.Fatalf("unscoped repository query carries a grant predicate:\n%s", unscopedCypher)
	}

	packageCypher, packageParams := BuildResolveNameFirstPackageQuery("billing", true, 10)
	for _, want := range []string{"MATCH (p:Package)", "toLower(p.normalized_name) = $name", "p.visibility = 'public'", "ORDER BY p.ecosystem", "LIMIT $limit"} {
		if !strings.Contains(packageCypher, want) {
			t.Fatalf("package query missing %q:\n%s", want, packageCypher)
		}
	}
	// Mixed-case normalized names (Maven/Go preserve raw case) must match the
	// lowercased input: the fold happens DB-side via toLower, so the pin above
	// — not the handler or fake — is what covers them.
	if strings.Contains(packageCypher, "p.normalized_name = $name") {
		t.Fatalf("package query compares case-sensitively, dropping case-preserving names:\n%s", packageCypher)
	}
	if got, want := packageParams["limit"], 11; got != want {
		t.Fatalf("package params[limit] = %#v, want %#v", got, want)
	}
	publicCypher, _ := BuildResolveNameFirstPackageQuery("billing", false, 10)
	if strings.Contains(publicCypher, "p.visibility") {
		t.Fatalf("unscoped package query carries a visibility predicate:\n%s", publicCypher)
	}
}
