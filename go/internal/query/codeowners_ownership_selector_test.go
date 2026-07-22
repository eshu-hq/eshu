// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// codeownersSlugSelector is a human slug (Repository.name), the shape
// #5606/#5471 report as broken: it never matched the canonical
// Repository.id{$repo_id} anchor codeownersOwnershipCypher and
// resolveEffectiveRepositoryOwner both require, so listOwnership always
// returned 0 rows for it before resolution was added.
const codeownersSlugSelector = "go_comprehensive"

// codeownersCanonicalRepoID is the canonical Repository.id the slug above
// resolves to, keyed the way DECLARES_CODEOWNER edges and access grants both
// use.
const codeownersCanonicalRepoID = "repository:r_8477a002"

func codeownersSelectorResolutionGraph() *recordingCodeownersGraphReader {
	return &recordingCodeownersGraphReader{
		selectorRows: []map[string]any{{"id": codeownersCanonicalRepoID}},
		runRows: []map[string]any{
			{"pattern": "*.go", "source_path": "CODEOWNERS", "order_index": int64(0), "owner_ref": "@org/team-a"},
		},
		singleRow: map[string]any{"owner_ref": "@org/team-a"},
	}
}

func decodeCodeownersOwnershipResponse(t *testing.T, body string) (ownership []CodeownersOwnershipRow, repositoryID string, effectiveOwner EffectiveRepositoryOwner) {
	t.Helper()
	var resp struct {
		Ownership      []CodeownersOwnershipRow `json:"ownership"`
		RepositoryID   string                   `json:"repository_id"`
		EffectiveOwner EffectiveRepositoryOwner `json:"effective_owner"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, body)
	}
	return resp.Ownership, resp.RepositoryID, resp.EffectiveOwner
}

// TestCodeownersOwnershipResolvesSlugSelectorToCanonicalRepositoryID is the
// #5606 RED->GREEN regression proof: a human slug (Repository.name, e.g.
// go_comprehensive) must resolve to the canonical Repository.id before
// listOwnership queries the DECLARES_CODEOWNER graph and
// resolveEffectiveRepositoryOwner, not get passed through verbatim into a
// MATCH (repo:Repository{id: $repo_id}) anchor it can never satisfy.
func TestCodeownersOwnershipResolvesSlugSelectorToCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	graph := codeownersSelectorResolutionGraph()
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id="+codeownersSlugSelector, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	ownership, repositoryID, _ := decodeCodeownersOwnershipResponse(t, w.Body.String())
	if len(ownership) != 1 {
		t.Fatalf("slug selector %q returned %d ownership rows, want 1: %+v", codeownersSlugSelector, len(ownership), ownership)
	}
	if got, want := repositoryID, codeownersCanonicalRepoID; got != want {
		t.Fatalf("repository_id = %q, want canonical id %q", got, want)
	}
	if got, want := graph.lastRunParams["repo_id"], codeownersCanonicalRepoID; got != want {
		t.Fatalf("DECLARES_CODEOWNER query saw repo_id = %#v, want canonical id %#v (slug leaked into the anchor unresolved)", got, want)
	}
}

// TestCodeownersOwnershipCanonicalSelectorStillResolves proves an
// already-canonical repository_id (the "repository:" prefix
// looksCanonicalRepositoryID recognizes, distinct from the "repo-" prefix the
// rest of this package's fixtures use) still reaches the graph unchanged
// through the new resolution step.
func TestCodeownersOwnershipCanonicalSelectorStillResolves(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{
		runRows: []map[string]any{
			{"pattern": "*.go", "source_path": "CODEOWNERS", "order_index": int64(0), "owner_ref": "@org/team-a"},
		},
		singleRow: map[string]any{"owner_ref": "@org/team-a"},
	}
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id="+codeownersCanonicalRepoID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	ownership, repositoryID, _ := decodeCodeownersOwnershipResponse(t, w.Body.String())
	if len(ownership) != 1 {
		t.Fatalf("canonical selector returned %d ownership rows, want 1: %+v", len(ownership), ownership)
	}
	if got, want := repositoryID, codeownersCanonicalRepoID; got != want {
		t.Fatalf("repository_id = %q, want %q", got, want)
	}
	if got, want := graph.lastRunParams["repo_id"], codeownersCanonicalRepoID; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
}

// TestCodeownersOwnershipScopedCallerGrantedCanonicalIDSeesSlugResolvedData
// is the granted-caller half of the #5606 fix: a scoped caller whose grant
// lists the canonical Repository.id must still see real ownership data when
// it requests the repository by slug, because the access check now runs
// against the resolved canonical id rather than the raw slug (which would
// never appear in a canonical-id-keyed grant list).
func TestCodeownersOwnershipScopedCallerGrantedCanonicalIDSeesSlugResolvedData(t *testing.T) {
	t.Parallel()

	graph := codeownersSelectorResolutionGraph()
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id="+codeownersSlugSelector, nil)
	scoped := scopedTestAuthContext("tenant-granted", []string{codeownersCanonicalRepoID})
	req = req.WithContext(ContextWithAuthContext(req.Context(), scoped))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	ownership, repositoryID, _ := decodeCodeownersOwnershipResponse(t, w.Body.String())
	if len(ownership) != 1 {
		t.Fatalf("scoped caller granted %q via slug %q saw %d rows, want 1: %+v", codeownersCanonicalRepoID, codeownersSlugSelector, len(ownership), ownership)
	}
	if got, want := repositoryID, codeownersCanonicalRepoID; got != want {
		t.Fatalf("repository_id = %q, want %q", got, want)
	}
}

// TestCodeownersOwnershipScopedCallerDeniedSlugGetsEmptyPageNotLeak pins the
// #5419 Phase 4b cross-tenant guard through the new resolution step: a
// scoped caller granted a different repository, requesting an ungranted
// repository by slug, must still get the bounded empty page -- never the
// resolved repository's real ownership/effective_owner, and never a 404 or
// 500 that would distinguish "exists but ungranted" from "granted but
// empty".
func TestCodeownersOwnershipScopedCallerDeniedSlugGetsEmptyPageNotLeak(t *testing.T) {
	t.Parallel()

	graph := codeownersSelectorResolutionGraph()
	correlations := &fakeCodeownersCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{RepositoryID: codeownersCanonicalRepoID, OwnerRef: "@org/team-manifest", Outcome: "exact"},
		},
	}
	mux := newCodeownersOwnershipMux(graph, correlations)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id="+codeownersSlugSelector, nil)
	scoped := scopedTestAuthContext("tenant-other", []string{"repo-a"})
	req = req.WithContext(ContextWithAuthContext(req.Context(), scoped))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	ownership, repositoryID, effectiveOwner := decodeCodeownersOwnershipResponse(t, w.Body.String())
	if len(ownership) != 0 {
		t.Fatalf("scoped caller not granted %q leaked ownership rows resolved from slug %q: %+v", codeownersCanonicalRepoID, codeownersSlugSelector, ownership)
	}
	if want := (EffectiveRepositoryOwner{}); effectiveOwner != want {
		t.Fatalf("scoped caller not granted %q leaked effective_owner: %+v, want zero value %+v", codeownersCanonicalRepoID, effectiveOwner, want)
	}
	if got, want := repositoryID, codeownersCanonicalRepoID; got != want {
		t.Fatalf("repository_id = %q, want resolved canonical id %q (empty page must still echo the resolved id, not the raw slug)", got, want)
	}
}

// TestCodeownersOwnershipUnknownSelectorReturnsNotFound proves a selector
// that matches no indexed repository at all -- neither a canonical id nor a
// resolvable alias -- surfaces the shared resolver's not-found error rather
// than silently returning a 0-row 200, mirroring
// resolveRepositorySelectorForRequestWithAccess's status mapping used by
// every other selector-accepting handler in this package.
func TestCodeownersOwnershipUnknownSelectorReturnsNotFound(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{}
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=no-such-selector", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
