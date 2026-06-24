// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingDependenciesGraphReader struct {
	runRows     []map[string]any
	lastCypher  string
	lastParams  map[string]any
	sawDeadline bool
	err         error
}

func (r *recordingDependenciesGraphReader) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	r.lastCypher = cypher
	r.lastParams = params
	_, r.sawDeadline = ctx.Deadline()
	if r.err != nil {
		return nil, r.err
	}
	return r.runRows, nil
}

func (*recordingDependenciesGraphReader) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func newDependenciesMux(reader GraphQuery) *http.ServeMux {
	handler := &DependenciesHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestDependenciesDefaultsToForwardWithDefaultLimit(t *testing.T) {
	t.Parallel()

	reader := &recordingDependenciesGraphReader{}
	mux := newDependenciesMux(reader)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/dependencies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !reader.sawDeadline {
		t.Fatal("dependency query context has no deadline; graph reads need a server-side read budget")
	}
	for _, fragment := range []string{
		"MATCH (src:Package)-[:HAS_VERSION]->(v:PackageVersion)-[:DECLARES_DEPENDENCY]->(d:PackageDependency)-[:DEPENDS_ON_PACKAGE]->(target:Package)",
		"RETURN 'forward' AS direction",
		"ORDER BY d.dependency_normalized, d.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	// Default limit 50 means the handler requests 51 (limit+1 truncation probe).
	if got, want := reader.lastParams["limit"], 51; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}

	var resp struct {
		Dependencies []DependencyRow `json:"dependencies"`
		Direction    string          `json:"direction"`
		Count        int             `json:"count"`
		Limit        int             `json:"limit"`
		Truncated    bool            `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Direction, "forward"; got != want {
		t.Fatalf("direction = %q, want %q", got, want)
	}
	if got, want := resp.Limit, dependenciesDefaultLimit; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false on empty result")
	}
	if len(resp.Dependencies) != 0 {
		t.Fatalf("len(dependencies) = %d, want 0", len(resp.Dependencies))
	}
}

func TestDependenciesRejectsInvalidDirectionAndLimit(t *testing.T) {
	t.Parallel()

	mux := newDependenciesMux(&recordingDependenciesGraphReader{})

	cases := map[string]string{
		"bad direction":     "/api/v0/dependencies?direction=sideways",
		"zero limit":        "/api/v0/dependencies?limit=0",
		"over max limit":    "/api/v0/dependencies?limit=201",
		"reverse no anchor": "/api/v0/dependencies?direction=reverse",
		"half cursor":       "/api/v0/dependencies?after_name=foo",
	}
	for name, target := range cases {
		name, target := name, target
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestDependenciesForwardAnchorsByPackageAndEcosystem(t *testing.T) {
	t.Parallel()

	reader := &recordingDependenciesGraphReader{
		runRows: []map[string]any{
			{
				"direction":          "forward",
				"anchor_package_id":  "npm://registry.npmjs.org/@eshu/core",
				"anchor_package":     "@eshu/core",
				"anchor_ecosystem":   "npm",
				"declaring_version":  "1.0.0",
				"related_package_id": "npm://registry.npmjs.org/left-pad",
				"related_package":    "left-pad",
				"related_ecosystem":  "npm",
				"dependency_range":   "^1.3.0",
				"dependency_type":    "runtime",
				"optional":           true,
				"edge_id":            "edge-1",
				"cursor_name":        "left-pad",
			},
		},
	}
	mux := newDependenciesMux(reader)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/dependencies?direction=forward&package=@eshu/core&ecosystem=npm&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(reader.lastCypher, "(src:Package {normalized_name: $package})") {
		t.Fatalf("cypher = %q, want indexed package anchor", reader.lastCypher)
	}
	if got, want := reader.lastParams["package"], "@eshu/core"; got != want {
		t.Fatalf("params[package] = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["ecosystem"], "npm"; got != want {
		t.Fatalf("params[ecosystem] = %#v, want %#v", got, want)
	}

	var resp struct {
		Dependencies []DependencyRow `json:"dependencies"`
		Direction    string          `json:"direction"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Dependencies), 1; got != want {
		t.Fatalf("len(dependencies) = %d, want %d", got, want)
	}
	row := resp.Dependencies[0]
	if got, want := row.RelatedPackage, "left-pad"; got != want {
		t.Fatalf("related_package = %q, want %q", got, want)
	}
	if got, want := row.DependencyRange, "^1.3.0"; got != want {
		t.Fatalf("dependency_range = %q, want %q", got, want)
	}
	if !row.Optional {
		t.Fatal("optional = false, want true")
	}
}

func TestDependenciesReverseAnchorsOnTargetPackage(t *testing.T) {
	t.Parallel()

	reader := &recordingDependenciesGraphReader{
		runRows: []map[string]any{
			{
				"direction":          "reverse",
				"anchor_package_id":  "npm://registry.npmjs.org/tslib",
				"anchor_package":     "tslib",
				"anchor_ecosystem":   "npm",
				"declaring_version":  "2.0.0",
				"related_package_id": "npm://registry.npmjs.org/@eshu/web",
				"related_package":    "@eshu/web",
				"related_ecosystem":  "npm",
				"dependency_range":   "^2.5.0",
				"edge_id":            "edge-9",
				"cursor_name":        "npm://registry.npmjs.org/@eshu/web",
			},
		},
	}
	mux := newDependenciesMux(reader)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/dependencies?direction=reverse&package=tslib&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"(target:Package {normalized_name: $package})<-[:DEPENDS_ON_PACKAGE]-(d:PackageDependency)<-[:DECLARES_DEPENDENCY]-(v:PackageVersion)",
		"RETURN 'reverse' AS direction",
		"v.package_id AS related_package_id",
		"ORDER BY v.package_id, d.uid",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}

	var resp struct {
		Dependencies []DependencyRow `json:"dependencies"`
		Direction    string          `json:"direction"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Direction, "reverse"; got != want {
		t.Fatalf("direction = %q, want %q", got, want)
	}
	if got, want := resp.Dependencies[0].RelatedPackage, "@eshu/web"; got != want {
		t.Fatalf("related_package = %q, want %q", got, want)
	}
}

func TestDependenciesTruncatesAndEmitsKeysetCursor(t *testing.T) {
	t.Parallel()

	reader := &recordingDependenciesGraphReader{
		runRows: []map[string]any{
			{
				"direction":          "forward",
				"related_package_id": "npm://registry.npmjs.org/aaa",
				"related_package":    "aaa",
				"edge_id":            "edge-a",
				"cursor_name":        "aaa",
			},
			{
				"direction":          "forward",
				"related_package_id": "npm://registry.npmjs.org/bbb",
				"related_package":    "bbb",
				"edge_id":            "edge-b",
				"cursor_name":        "bbb",
			},
		},
	}
	mux := newDependenciesMux(reader)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/dependencies?limit=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := reader.lastParams["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v (limit+1 truncation probe)", got, want)
	}

	var resp struct {
		Dependencies []DependencyRow   `json:"dependencies"`
		Truncated    bool              `json:"truncated"`
		NextCursor   map[string]string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Dependencies), 1; got != want {
		t.Fatalf("len(dependencies) = %d, want %d", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_name"], "aaa"; got != want {
		t.Fatalf("next_cursor[after_name] = %q, want %q", got, want)
	}
	if got, want := resp.NextCursor["after_edge"], "edge-a"; got != want {
		t.Fatalf("next_cursor[after_edge] = %q, want %q", got, want)
	}
}

func TestDependenciesForwardCursorThreadsKeysetParams(t *testing.T) {
	t.Parallel()

	reader := &recordingDependenciesGraphReader{}
	mux := newDependenciesMux(reader)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/dependencies?after_name=left-pad&after_edge=edge-1&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if want := "$after_name = '' OR d.dependency_normalized > $after_name OR (d.dependency_normalized = $after_name AND d.uid > $after_edge)"; !strings.Contains(reader.lastCypher, want) {
		t.Fatalf("cypher = %q, want keyset cursor fragment %q", reader.lastCypher, want)
	}
	if got, want := reader.lastParams["after_name"], "left-pad"; got != want {
		t.Fatalf("params[after_name] = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["after_edge"], "edge-1"; got != want {
		t.Fatalf("params[after_edge] = %#v, want %#v", got, want)
	}
}

func TestDependenciesBackendUnavailableWhenGraphMissing(t *testing.T) {
	t.Parallel()

	handler := &DependenciesHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/dependencies?limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
