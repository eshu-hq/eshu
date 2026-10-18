// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// tenantAScope attaches tenant A's scoped grant to the request context.
func tenantAScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, tenantAScopedAuthContext())
}

// runRegistryFailureRoutes drives every route through every failure case. It
// swaps packageregTracer, so callers must not be parallel.
func runRegistryFailureRoutes(t *testing.T, routes []failure.Route) {
	t.Helper()
	failure.Run(t, failure.Suite{HandlerTracer: &packageregTracer, Routes: routes})
}

// registryFailurePackageRow is one well-formed package row, so the packages
// read yields a result and the version-count read runs after it.
func registryFailurePackageRow() []map[string]any {
	return []map[string]any{{"package_id": "pkg:npm:lib", "ecosystem": "npm", "normalized_name": "lib", "visibility": "private"}}
}

// TestPackageRegistryReadFailuresAnswerFixedText drives every package
// registry failure step through a backend fault, a client cancel, and a stale
// reader (#7674). The scoped gate steps are authorization probes: a probe
// failure must answer an error status, never the empty page a denied or
// nonexistent anchor gets, so a failing probe cannot fail open or read as
// "not granted".
func TestPackageRegistryReadFailuresAnswerFixedText(t *testing.T) {
	privateAnchor := map[string][]map[string]any{
		packageRegistryAnchorVisibilityCypher: {{"visibility": "private"}},
	}
	runRegistryFailureRoutes(t, []failure.Route{
		{
			Name:    "packages query",
			Path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			Message: packageRegistryPackagesQueryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			Name:    "packages version counts",
			Path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			Message: packageRegistryPackageVersionCountsFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 2, rows: registryFailurePackageRow()}}
			},
		},
		{
			Name:    "packages scoped anchor visibility",
			Path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryPackagesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			Name:    "packages scoped anchor grant probe",
			Path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryPackagesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: privateAnchor},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "packages scoped name lookup",
			Path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryPackagesNameLookupFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			Name:    "packages scoped nonexistent name probe",
			Path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryPackagesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "packages scoped name batch probe",
			Path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryPackagesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j: &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: map[string][]map[string]any{
						packageRegistryNameAnchorVisibilityCypher: {{"package_id": "pkg:npm:lib", "visibility": "private"}},
					}},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "versions query",
			Path:    "/api/v0/package-registry/versions?package_id=pkg:npm:lib&limit=10",
			Message: packageRegistryVersionsQueryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			Name:    "versions scoped access check",
			Path:    "/api/v0/package-registry/versions?package_id=pkg:npm:lib&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryVersionsAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: privateAnchor},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "dependencies query",
			Path:    "/api/v0/package-registry/dependencies?package_id=pkg:npm:lib&limit=10",
			Message: packageRegistryDependenciesQueryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			Name:    "dependencies scoped version lookup",
			Path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryDependenciesVersionLookupFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			Name:    "dependencies scoped nonexistent version probe",
			Path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryDependenciesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "dependencies scoped anchor grant probe",
			Path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			Scope:   tenantAScope,
			Message: packageRegistryDependenciesAccessCheckFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{
					Neo4j: &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: map[string][]map[string]any{
						packageRegistryVersionAnchorPackageIDCypher: {{"package_id": "pkg:npm:lib"}},
						packageRegistryAnchorVisibilityCypher:       {{"visibility": "private"}},
					}},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			Name:    "correlations query",
			Path:    "/api/v0/package-registry/correlations?package_id=pkg:npm:lib&limit=10",
			Message: packageRegistryCorrelationsQueryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Correlations: failingRegistryCorrelations{err: err}} },
		},
		{
			Name:    "dependency chains query",
			Path:    "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=10",
			Message: packageRegistryDependencyChainsQueryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Correlations: failingRegistryCorrelations{err: err}} },
		},
		{
			Name:    "aggregate count",
			Path:    "/api/v0/package-registry/packages/count",
			Message: packageRegistryAggregateCountFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Aggregates: &stubPackageRegistryAggregateStore{countErr: err}}
			},
		},
		{
			Name:    "aggregate inventory",
			Path:    "/api/v0/package-registry/packages/inventory",
			Message: packageRegistryAggregateInventoryFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Aggregates: &stubPackageRegistryAggregateStore{inventoryErr: err}}
			},
		},
	})
}

// TestPackageRegistryScopedProbeFailureNeverAnswersDenial pins the fail-closed
// half of the gate contract: when every scoped access probe fails, the route
// answers 500 with its fixed message, not the 200 empty page (or a 403/404)
// that a denied grant or a nonexistent anchor answers.
func TestPackageRegistryScopedProbeFailureNeverAnswersDenial(t *testing.T) {
	for _, path := range []string{
		"/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
		"/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
		"/api/v0/package-registry/versions?package_id=pkg:npm:lib&limit=10",
		"/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
	} {
		t.Run(path, func(t *testing.T) {
			handler := &Handler{
				Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1},
				Correlations: failingRegistryCorrelations{err: errors.New("correlation store down")},
			}
			route := failure.Route{Name: path, Path: path, Scope: tenantAScope}
			rec, _ := failure.Serve(failure.Suite{HandlerTracer: &packageregTracer}, route, handler, false)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 (never the 200 empty page, 403, or 404 of a denial); body = %s",
					rec.Code, rec.Body.String())
			}
			if got := failure.Message(rec.Body.Bytes()); got == "" || got == "correlation store down" {
				t.Fatalf("body message = %q, want a fixed access-check message", got)
			}
		})
	}
}
