// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func scopedPackageRegistryRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
	}))
	return req
}

// TestPackageRegistryListPackagesMapsGraphReadAvailabilityErrors covers the
// primary package-anchor graph read in listPackages (package_registry.go).
func TestPackageRegistryListPackagesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryListPackagesMapsVersionCountGraphReadAvailabilityErrors
// covers the second (version-count) graph read attachPackageVersionCounts
// issues from listPackages.
func TestPackageRegistryListPackagesMapsVersionCountGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			reader := &recordingPackageRegistryGraphReader{
				runRowsQueue: [][]map[string]any{
					{
						{
							"package_id":      "package:npm:@eshu/core-api",
							"ecosystem":       "npm",
							"normalized_name": "core-api",
						},
					},
				},
				errByCall: map[int]error{2: test.err},
			}
			handler := &PackageRegistryHandler{Neo4j: reader}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryListVersionsMapsGraphReadAvailabilityErrors covers the
// graph read in listVersions (package_registry.go).
func TestPackageRegistryListVersionsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/versions?package_id=package:npm:@eshu/core-api&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryListDependenciesMapsGraphReadAvailabilityErrors covers
// the graph read in listDependencies (package_registry.go).
func TestPackageRegistryListDependenciesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryPackagesGatePackageIDAnchorMapsGraphReadAvailabilityErrors
// covers resolvePackageRegistryAnchorGate's graph visibility read from the
// package_id-anchored branch of packageRegistryPackagesGate
// (package_registry_scoped_gates.go), for a scoped caller.
func TestPackageRegistryPackagesGatePackageIDAnchorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=package:npm:@eshu/core-api&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryPackagesGateNameAnchorMapsGraphReadAvailabilityErrors
// covers packageRegistryNameAnchorCandidates's graph read from the
// name+ecosystem branch of packageRegistryPackagesGate
// (package_registry_scoped_gates.go), for a scoped caller.
func TestPackageRegistryPackagesGateNameAnchorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&name=core-api&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryVersionsGateMapsGraphReadAvailabilityErrors covers
// resolvePackageRegistryAnchorGate's graph visibility read from
// packageRegistryVersionsGate (package_registry_scoped_gates.go), for a
// scoped caller.
func TestPackageRegistryVersionsGateMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/versions?package_id=package:npm:@eshu/core-api&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryDependenciesGateVersionAnchorMapsGraphReadAvailabilityErrors
// covers packageRegistryVersionAnchorPackageID's graph read from
// packageRegistryDependenciesGate (package_registry_scoped_gates.go), for a
// scoped caller resolving a version_id anchor.
func TestPackageRegistryDependenciesGateVersionAnchorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/dependencies?version_id=package:npm:@eshu/core-api@1.0.0&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryDependenciesGateAnchorMapsGraphReadAvailabilityErrors
// covers resolvePackageRegistryAnchorGate's graph visibility read from the
// final anchor gate in packageRegistryDependenciesGate
// (package_registry_scoped_gates.go), for a scoped caller anchored directly
// on package_id (skipping version_id resolution).
func TestPackageRegistryDependenciesGateAnchorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/dependencies?package_id=package:npm:@eshu/core-api&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryDependenciesGateSentinelProbeMapsGraphReadAvailabilityErrors
// covers the sentinel-anchor correlation probe in packageRegistryDependenciesGate
// (package_registry_scoped_gates.go): a version_id that does not resolve to a
// package still issues the same visibility-lookup + correlation-probe
// sequence a resolving anchor would, against a sentinel package_id, and that
// probe's graph read must map the same way.
func TestPackageRegistryDependenciesGateSentinelProbeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			reader := &recordingPackageRegistryGraphReader{
				// Call 1: version_id anchor lookup resolves to nothing.
				runRowsQueue: [][]map[string]any{{}},
				// Call 2: sentinel-anchor visibility lookup fails.
				errByCall: map[int]error{2: test.err},
			}
			handler := &PackageRegistryHandler{Neo4j: reader}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := scopedPackageRegistryRequest(http.MethodGet, "/api/v0/package-registry/dependencies?version_id=package:npm:@eshu/core-api@1.0.0&limit=10")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
