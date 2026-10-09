// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"errors"
	"net/http"
	"testing"
)

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
	runRegistryFailureRoutes(t, []registryFailureRoute{
		{
			name:    "packages query",
			path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			message: packageRegistryPackagesQueryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			name:    "packages version counts",
			path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			message: packageRegistryPackageVersionCountsFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 2, rows: registryFailurePackageRow()}}
			},
		},
		{
			name:    "packages scoped anchor visibility",
			path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			scoped:  true,
			message: packageRegistryPackagesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			name:    "packages scoped anchor grant probe",
			path:    "/api/v0/package-registry/packages?package_id=pkg:npm:lib&limit=10",
			scoped:  true,
			message: packageRegistryPackagesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: privateAnchor},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			name:    "packages scoped name lookup",
			path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			scoped:  true,
			message: packageRegistryPackagesNameLookupFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			name:    "packages scoped nonexistent name probe",
			path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			scoped:  true,
			message: packageRegistryPackagesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			name:    "packages scoped name batch probe",
			path:    "/api/v0/package-registry/packages?ecosystem=npm&name=lib&limit=10",
			scoped:  true,
			message: packageRegistryPackagesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j: &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: map[string][]map[string]any{
						packageRegistryNameAnchorVisibilityCypher: {{"package_id": "pkg:npm:lib", "visibility": "private"}},
					}},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			name:    "versions query",
			path:    "/api/v0/package-registry/versions?package_id=pkg:npm:lib&limit=10",
			message: packageRegistryVersionsQueryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			name:    "versions scoped access check",
			path:    "/api/v0/package-registry/versions?package_id=pkg:npm:lib&limit=10",
			scoped:  true,
			message: packageRegistryVersionsAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1, answers: privateAnchor},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			name:    "dependencies query",
			path:    "/api/v0/package-registry/dependencies?package_id=pkg:npm:lib&limit=10",
			message: packageRegistryDependenciesQueryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Neo4j: &failingRegistryGraph{err: err}} },
		},
		{
			name:    "dependencies scoped version lookup",
			path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			scoped:  true,
			message: packageRegistryDependenciesVersionLookupFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Neo4j: &failingRegistryGraph{err: err, failCall: 1}, Correlations: failingRegistryCorrelations{err: err}}
			},
		},
		{
			name:    "dependencies scoped nonexistent version probe",
			path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			scoped:  true,
			message: packageRegistryDependenciesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:        &failingRegistryGraph{err: errors.New("unused"), failCall: -1},
					Correlations: failingRegistryCorrelations{err: err},
				}
			},
		},
		{
			name:    "dependencies scoped anchor grant probe",
			path:    "/api/v0/package-registry/dependencies?version_id=ver:npm:lib@1&limit=10",
			scoped:  true,
			message: packageRegistryDependenciesAccessCheckFailedMessage,
			handler: func(err error) *Handler {
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
			name:    "correlations query",
			path:    "/api/v0/package-registry/correlations?package_id=pkg:npm:lib&limit=10",
			message: packageRegistryCorrelationsQueryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Correlations: failingRegistryCorrelations{err: err}} },
		},
		{
			name:    "dependency chains query",
			path:    "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=10",
			message: packageRegistryDependencyChainsQueryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Correlations: failingRegistryCorrelations{err: err}} },
		},
		{
			name:    "aggregate count",
			path:    "/api/v0/package-registry/packages/count",
			message: packageRegistryAggregateCountFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Aggregates: &stubPackageRegistryAggregateStore{countErr: err}}
			},
		},
		{
			name:    "aggregate inventory",
			path:    "/api/v0/package-registry/packages/inventory",
			message: packageRegistryAggregateInventoryFailedMessage,
			handler: func(err error) *Handler {
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
			rec, _ := serveRegistryTraced(t, handler, path, true, false)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 (never the 200 empty page, 403, or 404 of a denial); body = %s",
					rec.Code, rec.Body.String())
			}
			if got := registryFailureMessage(rec.Body.Bytes()); got == "" || got == "correlation store down" {
				t.Fatalf("body message = %q, want a fixed access-check message", got)
			}
		})
	}
}
