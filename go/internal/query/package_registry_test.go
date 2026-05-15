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

type recordingPackageRegistryGraphReader struct {
	runRows    []map[string]any
	lastCypher string
	lastParams map[string]any
}

func (r *recordingPackageRegistryGraphReader) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	r.lastCypher = cypher
	r.lastParams = params
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
		runRows: []map[string]any{
			{
				"package_id":      "package:npm:@eshu/core-api",
				"ecosystem":       "npm",
				"registry":        "npmjs",
				"namespace":       "@eshu",
				"normalized_name": "core-api",
				"visibility":      "private",
				"version_count":   int64(3),
			},
			{
				"package_id":      "package:npm:@eshu/core-api-extra",
				"ecosystem":       "npm",
				"registry":        "npmjs",
				"namespace":       "@eshu",
				"normalized_name": "core-api-extra",
				"visibility":      "private",
				"version_count":   int64(1),
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
	for _, fragment := range []string{
		"MATCH (p:Package {ecosystem: $ecosystem})",
		"OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion)",
		"ORDER BY p.ecosystem, p.normalized_name, p.uid",
		"LIMIT $limit",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if _, ok := reader.lastParams["name"]; ok {
		t.Fatalf("params[name] = %#v, want absent for ecosystem-only scan", reader.lastParams["name"])
	}
	if got, want := reader.lastParams["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
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
				"version_id":    "package:npm:@eshu/core-api@1.0.0",
				"package_id":    "package:npm:@eshu/core-api",
				"version":       "1.0.0",
				"published_at":  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				"is_yanked":     false,
				"is_unlisted":   false,
				"is_deprecated": false,
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
	if got, want := resp.Versions[0].PublishedAt, "2026-05-01T00:00:00Z"; got != want {
		t.Fatalf("published_at = %q, want %q", got, want)
	}
}

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
		"MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion)",
		"MATCH (v)-[:DECLARES_DEPENDENCY]->(d:PackageDependency)-[:DEPENDS_ON_PACKAGE]->(target:Package)",
		"ORDER BY v.uid, d.dependency_type, d.dependency_normalized, d.uid",
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
	if !first.Optional {
		t.Fatal("optional = false, want true")
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
}
