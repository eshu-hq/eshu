// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// fakeManifestDependencyStore is the hermetic ManifestDependencyStore: rows
// are served verbatim, and every call is recorded for the no-store-call
// assertions on the grant short-circuits.
type fakeManifestDependencyStore struct {
	page    ManifestDependencyPage
	refused bool
	err     error
	calls   int
	last    fakeManifestCall
}

type fakeManifestCall struct {
	scopeID      string
	cursorFactID string
	fetchLimit   int
	includeDev   bool
	hasCursor    bool
}

func (f *fakeManifestDependencyStore) ListRepositoryManifestDependencyFacts(
	_ context.Context,
	scopeID string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	fetchLimit int,
	includeDev bool,
) (ManifestDependencyPage, bool, error) {
	f.calls++
	f.last = fakeManifestCall{scopeID: scopeID, cursorFactID: cursorFactID, fetchLimit: fetchLimit, includeDev: includeDev, hasCursor: cursorObservedAt != nil}
	return f.page, f.refused, f.err
}

func manifestTestRows() []ManifestDependencyRow {
	return []ManifestDependencyRow{
		{PackageName: "runtime-pkg", DeclaredRange: "1.0.0", ManifestFile: "package.json", Ecosystem: "npm", PackageManager: "npm", Section: "dependencies", Scope: "runtime", FactID: "fact-1", ObservedAt: "2026-05-14T10:00:00Z"},
		{PackageName: "dev-pkg", DeclaredRange: "^2.0.0", ManifestFile: "packages/dev/package.json", Ecosystem: "npm", PackageManager: "npm", Section: "devDependencies", Scope: "dev", FactID: "fact-2", ObservedAt: "2026-05-14T10:00:01Z"},
	}
}

func manifestFreshPage() ManifestDependencyPage {
	return ManifestDependencyPage{
		Rows: manifestTestRows(),
		Freshness: ManifestDependencyFreshness{
			ScopeFound:         true,
			ActiveGenerationID: "gen-1",
			GenerationStatus:   "active",
			FreshnessState:     string(querycontract.FreshnessFresh),
		},
	}
}

func manifestServeRequest(t *testing.T, h *Handler, target string, authCtx *auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	recorder := httptest.NewRecorder()
	h.listManifestDependencies(recorder, req)
	return recorder
}

func manifestDecodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

func manifestScopedGrant(repositoryIDs ...string) *auth.AuthContext {
	return &auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: repositoryIDs,
	}
}

// TestListManifestDependenciesServesPageAndCursor pins the served shape: the
// row fields, the freshness behind the page, the truncation probe, and the
// (observed_at, fact_id) next cursor.
func TestListManifestDependenciesServesPageAndCursor(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store}

	recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=1", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := manifestDecodeBody(t, recorder)
	if body["repository_id"] != "repo-1" {
		t.Fatalf("repository_id = %v, want repo-1", body["repository_id"])
	}
	dependencies, ok := body["dependencies"].([]any)
	if !ok || len(dependencies) != 1 {
		t.Fatalf("dependencies = %v, want the first of 2 rows", body["dependencies"])
	}
	first, _ := dependencies[0].(map[string]any)
	if first["package_name"] != "runtime-pkg" || first["declared_range"] != "1.0.0" ||
		first["manifest_file"] != "package.json" || first["ecosystem"] != "npm" ||
		first["scope"] != "runtime" {
		t.Fatalf("first row = %v, want the runtime package.json row", first)
	}
	if body["count"] != float64(1) || body["limit"] != float64(1) || body["truncated"] != true {
		t.Fatalf("page = %v, want count 1 limit 1 truncated true", body)
	}
	cursor, ok := body["next_cursor"].(map[string]any)
	if !ok || cursor["after_fact_id"] != "fact-1" || cursor["after_observed_at"] != "2026-05-14T10:00:00Z" {
		t.Fatalf("next_cursor = %v, want the first row's keyset", body["next_cursor"])
	}
	freshness, ok := body["freshness"].(map[string]any)
	if !ok || freshness["scope_found"] != true || freshness["freshness_state"] != "fresh" {
		t.Fatalf("freshness = %v, want the active generation state", body["freshness"])
	}
	if store.calls != 1 || store.last.scopeID != "repo-1" || store.last.fetchLimit != 2 || store.last.hasCursor || store.last.includeDev {
		t.Fatalf("store call = %+v, want scope repo-1 fetchLimit 2 no cursor, runtime only", store.last)
	}
}

// TestListManifestDependenciesTruthEnvelope pins the envelope contract over
// the negotiated shape: the semantic_facts basis with a reason that says the
// rows are declared, not resolved.
func TestListManifestDependenciesTruthEnvelope(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", nil)
	req.Header.Set("Accept", "application/eshu.envelope+json")
	recorder := httptest.NewRecorder()
	h.listManifestDependencies(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data  map[string]any `json:"data"`
		Truth struct {
			Basis  string `json:"basis"`
			Reason string `json:"reason"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Truth.Basis != "semantic_facts" {
		t.Fatalf("truth.basis = %q, want semantic_facts", envelope.Truth.Basis)
	}
	for _, want := range []string{"declared", "resolved"} {
		if !strings.Contains(envelope.Truth.Reason, want) {
			t.Fatalf("truth.reason = %q, want it to mention declared-not-resolved", envelope.Truth.Reason)
		}
	}
	if envelope.Data["repository_id"] != "repo-1" {
		t.Fatalf("data.repository_id = %v, want repo-1", envelope.Data["repository_id"])
	}
}

// TestListManifestDependenciesCursorRoundTrip pins cursor paging: the
// emitted next_cursor feeds the following page, which serves the remaining
// row without truncation or a further cursor.
func TestListManifestDependenciesCursorRoundTrip(t *testing.T) {
	store := &fakeManifestDependencyStore{page: ManifestDependencyPage{
		Rows:      manifestTestRows()[1:],
		Freshness: manifestFreshPage().Freshness,
	}}
	h := &Handler{Store: store}

	recorder := manifestServeRequest(t, h,
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=1&after_observed_at=2026-05-14T10%3A00%3A00Z&after_fact_id=fact-1", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := manifestDecodeBody(t, recorder)
	dependencies, _ := body["dependencies"].([]any)
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %v, want the second row", body["dependencies"])
	}
	if body["truncated"] != false {
		t.Fatalf("truncated = %v, want false on the last page", body["truncated"])
	}
	if _, ok := body["next_cursor"]; ok {
		t.Fatalf("next_cursor = %v, want absent on the last page", body["next_cursor"])
	}
	if !store.last.hasCursor || store.last.cursorFactID != "fact-1" {
		t.Fatalf("store call = %+v, want the keyset cursor", store.last)
	}
}

// TestListManifestDependenciesIncludeDevFlag pins the dev-inclusion opt-in:
// absent defaults to runtime-only, an explicit true threads the flag to the
// store, and the response echoes the filter state.
func TestListManifestDependenciesIncludeDevFlag(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store}

	recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1&include_dev_dependencies=true", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !store.last.includeDev {
		t.Fatalf("store call = %+v, want includeDev true", store.last)
	}
	body := manifestDecodeBody(t, recorder)
	if body["include_dev_dependencies"] != true {
		t.Fatalf("include_dev_dependencies = %v, want true echoed", body["include_dev_dependencies"])
	}
}

// TestListManifestDependenciesRejectsBadInput pins the bounded contract:
// repository_id is required, limit stays in 1..200, and the cursor halves
// travel together with an RFC 3339 observed_at.
func TestListManifestDependenciesRejectsBadInput(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store}

	for _, target := range []string{
		"/api/v0/repository/manifest-dependencies",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=0",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=201",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=nope",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&after_observed_at=2026-05-14T10%3A00%3A00Z",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&after_fact_id=fact-1",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&after_observed_at=not-a-time&after_fact_id=fact-1",
		"/api/v0/repository/manifest-dependencies?repository_id=repo-1&include_dev_dependencies=maybe",
	} {
		if recorder := manifestServeRequest(t, h, target, nil); recorder.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", target, recorder.Code)
		}
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0: bad input never reads", store.calls)
	}
}

// TestListManifestDependenciesGrantShortCircuits pins the grant contract: an
// empty scoped grant and an out-of-grant repository answer the same empty
// page a granted empty repository receives, without touching the store — a
// private or nonexistent repository is indistinguishable.
func TestListManifestDependenciesGrantShortCircuits(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store}

	empty := manifestScopedGrant()
	outOfGrant := manifestScopedGrant("other-repo")
	for _, grant := range []*auth.AuthContext{empty, outOfGrant} {
		recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", grant)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
		}
		body := manifestDecodeBody(t, recorder)
		dependencies, _ := body["dependencies"].([]any)
		if len(dependencies) != 0 || body["count"] != float64(0) || body["truncated"] != false {
			t.Fatalf("body = %v, want the empty page", body)
		}
		freshness, _ := body["freshness"].(map[string]any)
		if freshness["freshness_state"] != "unavailable" {
			t.Fatalf("freshness = %v, want unavailable with no read", body["freshness"])
		}
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0: denied grants never read", store.calls)
	}

	granted := manifestScopedGrant("repo-1")
	if recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", granted); recorder.Code != http.StatusOK {
		t.Fatalf("granted status = %d, want 200", recorder.Code)
	}
	if store.calls != 1 {
		t.Fatalf("granted store calls = %d, want 1", store.calls)
	}
}

// TestListManifestDependenciesStoreOutages pins the failure contract: a nil
// store answers 503, a refused delta base answers 503 without serving the
// partial page, and any other store error answers 500 with the fixed body,
// never the backend error.
func TestListManifestDependenciesStoreOutages(t *testing.T) {
	nilHandler := &Handler{}
	if recorder := manifestServeRequest(t, nilHandler, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", nil); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store status = %d, want 503", recorder.Code)
	}

	refused := &fakeManifestDependencyStore{refused: true, err: errors.New("union_base_missing")}
	h := &Handler{Store: refused}
	recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", nil)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("refused status = %d, want 503", recorder.Code)
	}
	var refusedBody map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &refusedBody); err != nil {
		t.Fatalf("decode refused body: %v", err)
	}

	failing := &fakeManifestDependencyStore{err: errors.New("connection refused: secret-backend-detail")}
	h = &Handler{Store: failing}
	recorder = manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failing store status = %d, want 500", recorder.Code)
	}
	if got := recorder.Body.String(); got == "connection refused: secret-backend-detail" || len(got) == 0 {
		t.Fatalf("error body leaks the backend error or is empty: %q", got)
	}
}

// TestListManifestDependenciesCapabilityGate pins the profile contract: a
// profile without support answers 501 naming the capability.
func TestListManifestDependenciesCapabilityGate(t *testing.T) {
	store := &fakeManifestDependencyStore{page: manifestFreshPage()}
	h := &Handler{Store: store, Profile: querycontract.ProfileLocalLightweight}

	recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1", nil)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", recorder.Code)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0 behind the capability gate", store.calls)
	}
}

// TestManifestDependencyEcosystemMapping pins the manager-to-ecosystem map:
// canonical spellings normalize, the sbt manager joins the maven family it
// feeds, and an unknown manager yields no ecosystem while the exact manager
// stays on the row.
func TestManifestDependencyEcosystemMapping(t *testing.T) {
	for manager, want := range map[string]string{
		"npm":    "npm",
		"pypi":   "pypi",
		"gradle": "maven",
		"maven":  "maven",
		"sbt":    "maven",
		"go":     "gomod",
	} {
		if got := manifestDependencyEcosystem(manager); got != want {
			t.Fatalf("ecosystem(%q) = %q, want %q", manager, got, want)
		}
	}
	if got := manifestDependencyEcosystem("mystery-manager"); got != "" {
		t.Fatalf("ecosystem(mystery-manager) = %q, want empty", got)
	}
}

// TestManifestDependencyEcosystemMappingTracksManagerExpansion pins the sbt
// fallback against the family expansion it inverts: if a new manager joins
// the maven family, the mapping must cover it explicitly instead of
// drifting.
func TestManifestDependencyEcosystemMappingTracksManagerExpansion(t *testing.T) {
	managers := packageidentity.ManifestPackageManagersForEcosystem("maven")
	covered := map[string]bool{"gradle": true, "maven": true, "sbt": true}
	if len(managers) != len(covered) {
		t.Fatalf("maven family managers = %v, want exactly %v", managers, covered)
	}
	for _, manager := range managers {
		if !covered[manager] {
			t.Fatalf("maven family manager %q has no ecosystem mapping", manager)
		}
	}
}

// TestManifestDependencyRowMapping pins the payload contract: the top-level
// entity fields and the entity_metadata signal fields land on the row, and
// the dev-or-runtime signal normalizes to the query-facing scope.
func TestManifestDependencyRowMapping(t *testing.T) {
	payload := map[string]any{
		"entity_name":   "left-pad",
		"relative_path": "package.json",
		"entity_metadata": map[string]any{
			"config_kind":            "dependency",
			"package_manager":        "npm",
			"version":                "^1.0.0",
			"section":                "dependencies",
			"dependency_scope":       "runtime",
			"development_dependency": false,
		},
	}
	row := manifestDependencyRow(payload, "fact-9", "gen-1", time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC))
	if row.PackageName != "left-pad" || row.DeclaredRange != "^1.0.0" || row.ManifestFile != "package.json" ||
		row.Ecosystem != "npm" || row.PackageManager != "npm" || row.Section != "dependencies" ||
		row.Scope != "runtime" || row.FactID != "fact-9" ||
		row.ObservedAt != "2026-05-14T10:00:00Z" {
		t.Fatalf("row = %+v, want the mapped payload", row)
	}

	bare := manifestDependencyRow(map[string]any{"entity_name": "x"}, "fact-x", "", time.Time{})
	if bare.Scope != "runtime" || bare.Section != "" || bare.Ecosystem != "" || bare.ObservedAt != "" {
		t.Fatalf("bare row = %+v, want runtime scope and no invented claims", bare)
	}
}

// TestManifestDependencyScopeNormalization pins the dev-or-runtime
// normalization: a development dependency maps to dev, a test-scoped one to
// test, and everything else stays runtime.
func TestManifestDependencyScopeNormalization(t *testing.T) {
	for _, tc := range []struct {
		development bool
		raw         string
		want        string
	}{
		{false, "", "runtime"},
		{false, "runtime", "runtime"},
		{true, "", "dev"},
		{true, "dev", "dev"},
		{true, "dev_optional", "dev"},
		{true, "test", "test"},
	} {
		if got := manifestDependencyScope(tc.development, tc.raw); got != tc.want {
			t.Fatalf("scope(dev=%v, raw=%q) = %q, want %q", tc.development, tc.raw, got, tc.want)
		}
	}
}
