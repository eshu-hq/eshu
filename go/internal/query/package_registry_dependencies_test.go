// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPackageRegistryListDependenciesRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Neo4j: &recordingPackageRegistryGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/dependencies?limit=10",
		"/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
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

func TestPackageRegistryListDependenciesUsesPackageOrVersionAnchor(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{
		runRows: []map[string]any{
			{
				"dependency_id":         "dep-1",
				"source_package_id":     "package:npm:@eshu/core-api",
				"source_version_id":     "package:npm:@eshu/core-api@1.0.0",
				"version":               "1.0.0",
				"dependency_package_id": "package:npm:left-pad",
				"dependency_ecosystem":  "npm",
				"dependency_registry":   "npmjs",
				"dependency_namespace":  "",
				"dependency_normalized": "left-pad",
				"dependency_purl":       "pkg:npm/left-pad",
				"dependency_bom_ref":    "pkg:npm/left-pad",
				"dependency_manager":    "npm",
				"dependency_range":      "^1.3.0",
				"dependency_type":       "runtime",
				"target_framework":      "node18",
				"marker":                "optional peer fallback",
				"optional":              true,
				"excluded":              false,
				"source_confidence":     "reported",
				"collector_kind":        "package_registry",
				"collector_instance_id": "package-registry-collector-1",
				"correlation_anchors":   []any{"package:npm:@eshu/core-api", "package:npm:left-pad"},
			},
			{
				"dependency_id":         "dep-2",
				"source_package_id":     "package:npm:@eshu/core-api",
				"source_version_id":     "package:npm:@eshu/core-api@1.1.0",
				"dependency_package_id": "package:npm:debug",
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"MATCH (d:PackageDependency)",
		"WHERE d.package_id = $package_id",
		"MATCH (d)-[:DEPENDS_ON_PACKAGE]->(target:Package)",
		"d.uid IS NOT NULL",
		"d.package_id IS NOT NULL",
		"d.version_id IS NOT NULL",
		"target.uid IS NOT NULL",
		"d.dependency_purl AS dependency_purl",
		"d.dependency_bom_ref AS dependency_bom_ref",
		"d.dependency_manager AS dependency_manager",
		"ORDER BY d.version_id, d.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if got, want := reader.lastParams["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}

	var resp struct {
		Dependencies []PackageRegistryDependencyResult `json:"dependencies"`
		Count        int                               `json:"count"`
		Limit        int                               `json:"limit"`
		Truncated    bool                              `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Dependencies), 1; got != want {
		t.Fatalf("len(dependencies) = %d, want %d", got, want)
	}
	first := resp.Dependencies[0]
	if got, want := first.DependencyPackageID, "package:npm:left-pad"; got != want {
		t.Fatalf("dependency_package_id = %q, want %q", got, want)
	}
	if got, want := first.DependencyType, "runtime"; got != want {
		t.Fatalf("dependency_type = %q, want %q", got, want)
	}
	if got, want := first.DependencyPURL, "pkg:npm/left-pad"; got != want {
		t.Fatalf("dependency_purl = %q, want %q", got, want)
	}
	if got, want := first.DependencyBOMRef, "pkg:npm/left-pad"; got != want {
		t.Fatalf("dependency_bom_ref = %q, want %q", got, want)
	}
	if got, want := first.DependencyManager, "npm"; got != want {
		t.Fatalf("dependency_manager = %q, want %q", got, want)
	}
	if !first.Optional {
		t.Fatal("optional = false, want true")
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestPackageRegistryListDependenciesReturnsEmptySparsePackageQuickly(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependencies?package_id=npm://registry.npmjs.org/lodash&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"MATCH (d:PackageDependency)",
		"WHERE d.package_id = $package_id",
		"MATCH (d)-[:DEPENDS_ON_PACKAGE]->(target:Package)",
		"ORDER BY d.version_id, d.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if strings.Contains(reader.lastCypher, "MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion)") {
		t.Fatalf("cypher = %q, must not expand every package version before discovering sparse dependencies", reader.lastCypher)
	}
	if !reader.sawDeadline {
		t.Fatal("dependency query context has no deadline; sparse reads need a server-side read budget")
	}

	var resp struct {
		Dependencies []PackageRegistryDependencyResult `json:"dependencies"`
		Count        int                               `json:"count"`
		Limit        int                               `json:"limit"`
		Truncated    bool                              `json:"truncated"`
		NextCursor   map[string]string                 `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := len(resp.Dependencies); got != 0 {
		t.Fatalf("len(dependencies) = %d, want 0", got)
	}
	if got := resp.Count; got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
	if got, want := resp.Limit, 5; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if resp.NextCursor != nil {
		t.Fatalf("next_cursor = %#v, want absent", resp.NextCursor)
	}
}

func TestPackageRegistryListDependenciesUsesBothAnchorsWhenProvided(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api&version_id=package:npm:@eshu/core-api@1.0.0&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, want := range []string{
		"WHERE d.version_id = $version_id",
		"($package_id = '' OR d.package_id = $package_id)",
		"($version_id = '' OR d.version_id = $version_id)",
	} {
		if !strings.Contains(reader.lastCypher, want) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, want)
		}
	}
	if strings.Contains(reader.lastCypher, "MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion") {
		t.Fatalf("cypher = %q, must not expand package versions when an exact version anchor is provided", reader.lastCypher)
	}
	if got, want := reader.lastParams["package_id"], "package:npm:@eshu/core-api"; got != want {
		t.Fatalf("params[package_id] = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["version_id"], "package:npm:@eshu/core-api@1.0.0"; got != want {
		t.Fatalf("params[version_id] = %#v, want %#v", got, want)
	}
}

func TestPackageRegistryListDependenciesReturnsCursorForTruncatedPage(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{
		runRows: []map[string]any{
			{
				"dependency_id":         "dep-2",
				"source_package_id":     "package:npm:@eshu/core-api",
				"source_version_id":     "package:npm:@eshu/core-api@1.0.0",
				"dependency_package_id": "package:npm:debug",
			},
			{
				"dependency_id":         "dep-3",
				"source_package_id":     "package:npm:@eshu/core-api",
				"source_version_id":     "package:npm:@eshu/core-api@1.1.0",
				"dependency_package_id": "package:npm:left-pad",
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api&limit=1&after_version_id=package:npm:@eshu/core-api@0.9.0&after_dependency_id=dep-1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for key, want := range map[string]string{
		"after_version_id":    "package:npm:@eshu/core-api@0.9.0",
		"after_dependency_id": "dep-1",
	} {
		if got := reader.lastParams[key]; got != want {
			t.Fatalf("params[%s] = %#v, want %#v", key, got, want)
		}
	}
	if want := "d.version_id > $after_version_id OR (d.version_id = $after_version_id AND d.uid > $after_dependency_id)"; !strings.Contains(reader.lastCypher, want) {
		t.Fatalf("cypher = %q, want cursor fragment %q", reader.lastCypher, want)
	}

	var resp struct {
		Dependencies []PackageRegistryDependencyResult `json:"dependencies"`
		Truncated    bool                              `json:"truncated"`
		NextCursor   map[string]string                 `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_version_id"], "package:npm:@eshu/core-api@1.0.0"; got != want {
		t.Fatalf("next_cursor[after_version_id] = %q, want %q", got, want)
	}
	if got, want := resp.NextCursor["after_dependency_id"], "dep-2"; got != want {
		t.Fatalf("next_cursor[after_dependency_id] = %q, want %q", got, want)
	}
}
