// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// #5167 code-family batch 1, step 2: two-tenant grant proof for the three
// content-index code routes that share the `*Filters`/`*Where` SQL builder
// shape -- POST /api/v0/code/security/secrets/investigate,
// POST /api/v0/code/symbols/search, and POST /api/v0/code/structure/inventory.
//
// All three had the same hole: `if repoID != "" { repo_id = $n }` and nothing
// at all in the else branch, so a scoped caller who omitted repo_id read the
// whole content corpus. Secrets is the sharpest of the three -- its rows carry
// redacted secret line text -- which is why it leads the table.
//
// Every fake below mirrors the shipped SQL's real contract: a non-empty
// AllowedRepositoryIDs list restricts the scan, an empty one leaves it
// unrestricted. That is what makes both assertions mutation-sensitive:
//   - Remove a handler's codeContentGrantScope call and the list arrives
//     empty, the other tenant's row comes back, and the "never contains the
//     out-of-grant repository" assertion fails.
//   - Remove the empty-grant short-circuit and a grantless scoped caller
//     reaches the store with an empty list, which the SQL reads as
//     "unrestricted", so queried flips true and the corpus is returned.

type codeContentGrantStore interface {
	ContentStore
	boundRepositoryIDs() []string
	storeWasQueried() bool
}

// codeContentGrantRecorder is the shared record-and-filter behavior each
// route's fake store needs: it remembers the grant list the handler bound and
// applies it the way the shipped SQL does.
type codeContentGrantRecorder struct {
	bound   []string
	queried bool
}

func (r *codeContentGrantRecorder) record(repoID string, allowedRepositoryIDs []string) {
	r.queried = true
	r.bound = append([]string(nil), allowedRepositoryIDs...)
	_ = repoID
}

func (r *codeContentGrantRecorder) boundRepositoryIDs() []string { return r.bound }
func (r *codeContentGrantRecorder) storeWasQueried() bool        { return r.queried }

// codeContentGrantAdmits mirrors the `repo_id = $n` / `repo_id = ANY($n)`
// pair the four shipped SQL builders emit: an explicit repo_id anchors the
// scan, a non-empty grant list restricts it, and an empty grant list does not.
func codeContentGrantAdmits(rowRepoID, repoID string, allowedRepositoryIDs []string) bool {
	if anchor := strings.TrimSpace(repoID); anchor != "" && rowRepoID != anchor {
		return false
	}
	if len(allowedRepositoryIDs) == 0 {
		return true
	}
	return slices.Contains(allowedRepositoryIDs, rowRepoID)
}

type hardcodedSecretGrantStore struct {
	fakePortContentStore
	codeContentGrantRecorder
}

func (s *hardcodedSecretGrantStore) InvestigateHardcodedSecrets(
	_ context.Context,
	req hardcodedSecretInvestigationRequest,
) ([]hardcodedSecretFindingRow, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	rows := make([]hardcodedSecretFindingRow, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(repoID, req.RepoID, req.AllowedRepositoryIDs) {
			continue
		}
		rows = append(rows, hardcodedSecretFindingRow{
			RepoID:       repoID,
			RelativePath: "internal/config/keys.go",
			Language:     "go",
			LineNumber:   12,
			LineText:     `apiToken := "sk_live_redacted"`,
			FindingKind:  "api_token",
		})
	}
	return rows, nil
}

type symbolSearchGrantStore struct {
	fakePortContentStore
	codeContentGrantRecorder
}

func (s *symbolSearchGrantStore) SearchSymbols(
	_ context.Context,
	req symbolSearchRequest,
) ([]EntityContent, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	return codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs), nil
}

type structuralInventoryGrantStore struct {
	fakePortContentStore
	codeContentGrantRecorder
}

func (s *structuralInventoryGrantStore) InspectStructuralInventory(
	_ context.Context,
	req structuralInventoryRequest,
) ([]EntityContent, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	return codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs), nil
}

func (s *structuralInventoryGrantStore) CountStructuralInventoryByFile(
	_ context.Context,
	req structuralInventoryRequest,
) ([]StructuralInventoryFileCount, error) {
	s.record(req.RepoID, req.AllowedRepositoryIDs)
	counts := make([]StructuralInventoryFileCount, 0, 2)
	for _, entity := range codeContentGrantEntities(req.RepoID, req.AllowedRepositoryIDs) {
		counts = append(counts, StructuralInventoryFileCount{
			RepoID:        entity.RepoID,
			RelativePath:  entity.RelativePath,
			Language:      entity.Language,
			FunctionCount: 3,
		})
	}
	return counts, nil
}

func codeContentGrantEntities(repoID string, allowedRepositoryIDs []string) []EntityContent {
	entities := make([]EntityContent, 0, 2)
	for _, candidate := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(candidate, repoID, allowedRepositoryIDs) {
			continue
		}
		entities = append(entities, EntityContent{
			EntityID:     candidate + "#RefreshSession",
			RepoID:       candidate,
			RelativePath: "internal/auth/session.go",
			EntityType:   "Function",
			EntityName:   "RefreshSession",
			Language:     "go",
			StartLine:    10,
			EndLine:      20,
		})
	}
	return entities
}

type codeContentGrantRoute struct {
	name     string
	path     string
	body     map[string]any
	newStore func() codeContentGrantStore
}

func codeContentGrantRoutes() []codeContentGrantRoute {
	return []codeContentGrantRoute{
		{
			name:     "investigate_hardcoded_secrets",
			path:     "/api/v0/code/security/secrets/investigate",
			body:     map[string]any{},
			newStore: func() codeContentGrantStore { return &hardcodedSecretGrantStore{} },
		},
		{
			name:     "search_symbols",
			path:     "/api/v0/code/symbols/search",
			body:     map[string]any{"symbol": "RefreshSession"},
			newStore: func() codeContentGrantStore { return &symbolSearchGrantStore{} },
		},
		{
			name:     "inspect_structural_inventory",
			path:     "/api/v0/code/structure/inventory",
			body:     map[string]any{"inventory_kind": "entity", "language": "go"},
			newStore: func() codeContentGrantStore { return &structuralInventoryGrantStore{} },
		},
		{
			name:     "inspect_structural_inventory_function_count_by_file",
			path:     "/api/v0/code/structure/inventory",
			body:     map[string]any{"inventory_kind": "function_count_by_file", "language": "go"},
			newStore: func() codeContentGrantStore { return &structuralInventoryGrantStore{} },
		},
	}
}

func TestCodeContentRoutesFilterByRepositoryGrant(t *testing.T) {
	t.Parallel()

	for _, route := range codeContentGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := route.newStore()
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got := store.boundRepositoryIDs(); !slices.Equal(got, []string{codeGrantGrantedRepo}) {
				t.Fatalf("AllowedRepositoryIDs = %#v, want [%q] bound into the content query", got, codeGrantGrantedRepo)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) {
				t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
			}
			if strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
			}
		})
	}
}

func TestCodeContentRoutesEmptyGrantSkipsTheContentRead(t *testing.T) {
	t.Parallel()

	for _, route := range codeContentGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := route.newStore()
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopedAuthContext(nil)
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if store.storeWasQueried() {
				t.Fatal("store was queried, want no read at all -- an empty scoped grant must skip the content read, not query then filter to empty")
			}
			body := rec.Body.String()
			if strings.Contains(body, codeGrantGrantedRepo) || strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked rows for an empty-grant caller: %s", body)
			}
		})
	}
}

func TestCodeContentRoutesSharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, route := range codeContentGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := route.newStore()
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := newCodeGrantRouteRequest(t, route.path, route.body, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got := store.boundRepositoryIDs(); len(got) != 0 {
				t.Fatalf("AllowedRepositoryIDs = %#v, want empty for an unscoped caller -- the shared-key read must stay unrestricted", got)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) || !strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("unscoped response lost rows: %s", body)
			}
		})
	}
}

// TestCodeContentFiltersBindTheGrantInTheShippedSQL is the guard the handler
// tests above cannot be: they drive fake stores, so no SQL text is ever built.
// Delete a builder's grant branch and every handler test still passes. This
// asserts the predicate against the shipped builders themselves, the same way
// TestFreshnessGrantPredicatesArePresentInTheShippedSQL
// (go/internal/storage/postgres/generation_lifecycle_grant_test.go) guards the
// freshness statements.
func TestCodeContentFiltersBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	grant := []string{codeGrantGrantedRepo}
	for _, tc := range []struct {
		name     string
		scoped   func() ([]string, []any)
		anchored func() []string
		want     string
	}{
		{
			name: "hardcoded_secrets",
			scoped: func() ([]string, []any) {
				filters, args, _ := hardcodedSecretFilters(hardcodedSecretInvestigationRequest{AllowedRepositoryIDs: grant})
				return filters, args
			},
			anchored: func() []string {
				filters, _, _ := hardcodedSecretFilters(hardcodedSecretInvestigationRequest{RepoID: codeGrantGrantedRepo})
				return filters
			},
			want: "repo_id = ANY($1)",
		},
		{
			name: "symbol_search",
			scoped: func() ([]string, []any) {
				filters, args, _ := symbolSearchFilters(symbolSearchRequest{Symbol: "RefreshSession", AllowedRepositoryIDs: grant})
				return filters, args
			},
			anchored: func() []string {
				filters, _, _ := symbolSearchFilters(symbolSearchRequest{Symbol: "RefreshSession", RepoID: codeGrantGrantedRepo})
				return filters
			},
			want: "repo_id = ANY($2)",
		},
		{
			name: "structural_inventory",
			scoped: func() ([]string, []any) {
				return structuralInventoryWhere(structuralInventoryRequest{AllowedRepositoryIDs: grant})
			},
			anchored: func() []string {
				where, _ := structuralInventoryWhere(structuralInventoryRequest{RepoID: codeGrantGrantedRepo})
				return where
			},
			want: "repo_id = ANY($1)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filters, args := tc.scoped()
			if !slices.Contains(filters, tc.want) {
				t.Fatalf("%s builder = %#v, want a %q grant predicate; without it a scoped caller's grant is resolved but never applied", tc.name, filters, tc.want)
			}
			assertBoundRepositoryGrantArray(t, args, grant)

			// A caller who named one repository must keep the single-repo
			// equality predicate, not gain a second, wider ANY() scan.
			for _, filter := range tc.anchored() {
				if strings.Contains(filter, "repo_id = ANY(") {
					t.Fatalf("%s builder emitted %q for a repo-anchored request; the grant list must not widen an anchored scan", tc.name, filter)
				}
			}
		})
	}
}

// symbolNameFallbackGrantStore drives the second read path behind
// POST /api/v0/code/symbols/search. When h.Content does not satisfy
// symbolContentSearcher, symbolSearchResults falls back to
// SearchEntitiesByName, which takes ONE repository at a time -- so a
// corpus-wide scoped search has to iterate the granted repositories itself. An
// unbound fallback asks for repository "" instead, which this store answers
// with every tenant's symbol, the same way the all-repository content query
// does.
type symbolNameFallbackGrantStore struct {
	fakePortContentStore
	askedRepoIDs []string
}

func (s *symbolNameFallbackGrantStore) SearchEntitiesByName(
	_ context.Context,
	repoID, _, _ string,
	_ int,
) ([]EntityContent, error) {
	s.askedRepoIDs = append(s.askedRepoIDs, repoID)
	return codeContentGrantEntities(repoID, nil), nil
}

func runSymbolNameFallbackSearch(
	t *testing.T,
	store *symbolNameFallbackGrantStore,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, "/api/v0/code/symbols/search", map[string]any{"symbol": "RefreshSession"}, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSymbolNameFallbackIteratesOnlyGrantedRepositories(t *testing.T) {
	t.Parallel()

	store := &symbolNameFallbackGrantStore{}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runSymbolNameFallbackSearch(t, store, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !slices.Equal(store.askedRepoIDs, []string{codeGrantGrantedRepo}) {
		t.Fatalf("SearchEntitiesByName repositories = %#v, want [%q]; the fallback must iterate the grant, never ask for every repository", store.askedRepoIDs, codeGrantGrantedRepo)
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedRepo) {
		t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
	}
	if strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("the name fallback leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
	}
}

func TestSymbolNameFallbackEmptyGrantSkipsTheLookup(t *testing.T) {
	t.Parallel()

	store := &symbolNameFallbackGrantStore{}
	auth := codeGrantScopedAuthContext(nil)
	rec := runSymbolNameFallbackSearch(t, store, &auth)

	if len(store.askedRepoIDs) != 0 {
		t.Fatalf("SearchEntitiesByName was called with %#v; a grantless scoped caller must not reach the content store", store.askedRepoIDs)
	}
	body := rec.Body.String()
	if strings.Contains(body, codeGrantGrantedRepo) || strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("response leaked rows for an empty-grant caller: %s", body)
	}
}

func TestSymbolNameFallbackSharedKeySearchIsUnchanged(t *testing.T) {
	t.Parallel()

	store := &symbolNameFallbackGrantStore{}
	rec := runSymbolNameFallbackSearch(t, store, nil)

	if !slices.Equal(store.askedRepoIDs, []string{""}) {
		t.Fatalf("SearchEntitiesByName repositories = %#v, want [\"\"]; an unscoped caller keeps the all-repository lookup", store.askedRepoIDs)
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedRepo) || !strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("unscoped name fallback lost rows: %s", body)
	}
}
