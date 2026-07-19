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
	"time"
)

// recordingPackageRegistryGraphReader is a test double for GraphQuery.
//
// listPackages now issues up to two Run calls: the package anchor read, then
// (when the page is non-empty) a separate scoped packageRegistryVersionCountsCypher
// read (see attachPackageVersionCounts). runRowsQueue lets a test supply a
// distinct result per call, in order; once exhausted (or if never set), Run
// falls back to the single runRows value for every remaining call, which
// keeps every single-call-per-test-case (versions, dependencies, and empty
// pages that skip the count round trip) unchanged.
type recordingPackageRegistryGraphReader struct {
	runRows      []map[string]any
	runRowsQueue [][]map[string]any
	lastCypher   string
	lastParams   map[string]any
	cypherCalls  []string
	paramsCalls  []map[string]any
	sawDeadline  bool
}

func (r *recordingPackageRegistryGraphReader) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	r.lastCypher = cypher
	r.lastParams = params
	r.cypherCalls = append(r.cypherCalls, cypher)
	r.paramsCalls = append(r.paramsCalls, params)
	_, r.sawDeadline = ctx.Deadline()
	if len(r.runRowsQueue) > 0 {
		next := r.runRowsQueue[0]
		r.runRowsQueue = r.runRowsQueue[1:]
		return next, nil
	}
	return r.runRows, nil
}

func (*recordingPackageRegistryGraphReader) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestPackageRegistryListPackagesRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Neo4j: &recordingPackageRegistryGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/packages?limit=10",
		"/api/v0/package-registry/packages?ecosystem=npm",
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

func TestPackageRegistryListPackagesNamesMissingEcosystem(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Neo4j: &recordingPackageRegistryGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?name=core-api&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := w.Body.String(), "ecosystem is required when name is provided"; !strings.Contains(got, want) {
		t.Fatalf("body = %s, want %q", got, want)
	}
}

func TestPackageRegistryListPackagesUsesIndexedPackageScopeAndTruncates(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{
		runRowsQueue: [][]map[string]any{
			{
				{
					"package_id":         "package:npm:@eshu/core-api",
					"ecosystem":          "npm",
					"registry":           "npmjs",
					"namespace":          "@eshu",
					"normalized_name":    "core-api",
					"purl":               "pkg:npm/%40eshu/core-api",
					"bom_ref":            "pkg:npm/%40eshu/core-api",
					"package_manager":    "npm",
					"source_path":        "package.json",
					"source_specific_id": "npm:@eshu/core-api",
					"visibility":         "private",
				},
				{
					"package_id":      "package:npm:@eshu/core-api-extra",
					"ecosystem":       "npm",
					"registry":        "npmjs",
					"namespace":       "@eshu",
					"normalized_name": "core-api-extra",
					"visibility":      "private",
				},
			},
			{
				{
					"package_id":    "package:npm:@eshu/core-api",
					"version_count": int64(3),
				},
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&limit=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(reader.cypherCalls), 2; got != want {
		t.Fatalf("len(cypherCalls) = %d, want %d (anchor read + scoped version-count read); calls = %#v", got, want, reader.cypherCalls)
	}
	anchorCypher := reader.cypherCalls[0]
	for _, fragment := range []string{
		"MATCH (p:Package {ecosystem: $ecosystem})",
		"RETURN p.uid AS package_id",
		"p.purl AS purl",
		"p.bom_ref AS bom_ref",
		"p.package_manager AS package_manager",
		"ORDER BY p.ecosystem, p.normalized_name, p.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(anchorCypher, fragment) {
			t.Fatalf("anchor cypher = %q, want fragment %q", anchorCypher, fragment)
		}
	}
	for _, forbidden := range []string{
		"OPTIONAL MATCH",
		"count(v)",
		"WITH p, count(v)",
	} {
		if strings.Contains(anchorCypher, forbidden) {
			t.Fatalf("anchor cypher = %q, must not contain %q: NornicDB's OPTIONAL MATCH + count(v) silently drops every zero-version package instead of returning version_count 0 (docs/public/reference/nornicdb-pitfalls.md)", anchorCypher, forbidden)
		}
	}
	if _, ok := reader.paramsCalls[0]["name"]; ok {
		t.Fatalf("params[name] = %#v, want absent for ecosystem-only scan", reader.paramsCalls[0]["name"])
	}
	if got, want := reader.paramsCalls[0]["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}

	countCypher := reader.cypherCalls[1]
	for _, fragment := range []string{
		"UNWIND $package_ids AS candidate_package_id",
		"MATCH (p:Package {uid: candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion)",
		"RETURN p.uid AS package_id, count(r) AS version_count",
	} {
		if !strings.Contains(countCypher, fragment) {
			t.Fatalf("count cypher = %q, want fragment %q", countCypher, fragment)
		}
	}
	countIDs, _ := reader.paramsCalls[1]["package_ids"].([]string)
	if got, want := countIDs, []string{"package:npm:@eshu/core-api"}; !equalStringSlices(got, want) {
		t.Fatalf("count params[package_ids] = %#v, want %#v (only the truncated page's package, not the dropped extra)", got, want)
	}

	var resp struct {
		Packages  []PackageRegistryPackageResult `json:"packages"`
		Count     int                            `json:"count"`
		Limit     int                            `json:"limit"`
		Truncated bool                           `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Packages), 1; got != want {
		t.Fatalf("len(packages) = %d, want %d", got, want)
	}
	if got, want := resp.Packages[0].PackageID, "package:npm:@eshu/core-api"; got != want {
		t.Fatalf("package_id = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].PURL, "pkg:npm/%40eshu/core-api"; got != want {
		t.Fatalf("purl = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].BOMRef, "pkg:npm/%40eshu/core-api"; got != want {
		t.Fatalf("bom_ref = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].PackageManager, "npm"; got != want {
		t.Fatalf("package_manager = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].SourcePath, "package.json"; got != want {
		t.Fatalf("source_path = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].SourceSpecificID, "npm:@eshu/core-api"; got != want {
		t.Fatalf("source_specific_id = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].VersionCount, 3; got != want {
		t.Fatalf("version_count = %d, want %d (from the scoped count read, not the anchor row)", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestPackageRegistryListVersionsRequiresPackageScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Neo4j: &recordingPackageRegistryGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/versions?limit=10",
		"/api/v0/package-registry/versions?package_id=package:npm:@eshu/core-api",
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

func TestPackageRegistryListVersionsUsesPackageUIDAnchor(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{
		runRows: []map[string]any{
			{
				"version_id":      "package:npm:@eshu/core-api@1.0.0",
				"package_id":      "package:npm:@eshu/core-api",
				"version":         "1.0.0",
				"purl":            "pkg:npm/%40eshu/core-api@1.0.0",
				"bom_ref":         "pkg:npm/%40eshu/core-api@1.0.0",
				"package_manager": "npm",
				"published_at":    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				"is_yanked":       false,
				"is_unlisted":     false,
				"is_deprecated":   false,
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/versions?package_id=package:npm:@eshu/core-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion)",
		"v.purl AS purl",
		"v.bom_ref AS bom_ref",
		"v.package_manager AS package_manager",
		"ORDER BY v.version, v.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if got, want := reader.lastParams["limit"], 11; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}

	var resp struct {
		Versions []PackageRegistryVersionResult `json:"versions"`
		Count    int                            `json:"count"`
		Limit    int                            `json:"limit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.Versions[0].VersionID, "package:npm:@eshu/core-api@1.0.0"; got != want {
		t.Fatalf("version_id = %q, want %q", got, want)
	}
	if got, want := resp.Versions[0].PURL, "pkg:npm/%40eshu/core-api@1.0.0"; got != want {
		t.Fatalf("purl = %q, want %q", got, want)
	}
	if got, want := resp.Versions[0].BOMRef, "pkg:npm/%40eshu/core-api@1.0.0"; got != want {
		t.Fatalf("bom_ref = %q, want %q", got, want)
	}
	if got, want := resp.Versions[0].PackageManager, "npm"; got != want {
		t.Fatalf("package_manager = %q, want %q", got, want)
	}
	if got, want := resp.Versions[0].PublishedAt, "2026-05-01T00:00:00Z"; got != want {
		t.Fatalf("published_at = %q, want %q", got, want)
	}
}
