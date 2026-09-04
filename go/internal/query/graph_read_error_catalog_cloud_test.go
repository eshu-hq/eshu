// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareEnvironmentsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CompareHandler{Neo4j: fakeGraphReaderWithSingle{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/compare/environments", strings.NewReader(
				`{"workload_id":"workload:svc","left":"staging","right":"production"}`,
			))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.compareEnvironments(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListEntitiesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &GraphEntityInventoryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listEntities(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListCloudResourcesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			store := &stubCloudResourceListStore{rows: []CloudResourceListIdentity{
				{UID: "cloud:a1", ResourceType: "aws_iam_role"},
			}}
			handler := &InfraHandler{
				Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				}},
				CloudResources: store,
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listCloudResources(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListCatalogMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listCatalog(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRepositoryStatsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReaderWithSingle{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-test/stats", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryStatsSelectorMapsGraphReadAvailabilityErrors covers the
// SELECTOR-resolution graph read, which the canonical-id test above never
// reaches: "repo-test" satisfies looksCanonicalRepositoryID and short-circuits
// before any graph read, so it only exercises the already-guarded
// repositoryStatsRepositoryRef path. A non-canonical selector ("my-repo-name")
// with no content store forces resolveRepositoryStatsPathSelector through the
// graph read, whose sentinel previously collapsed into HTTP 400.
func TestGetRepositoryStatsSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &RepositoryHandler{Neo4j: fakeGraphReaderWithSingle{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/my-repo-name/stats", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRepositoryContextMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReaderWithSingle{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-test/context", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRepositoryCoverageMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReaderWithSingle{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			// "repo-test" looks canonical (repo- prefix) and Content is nil, so
			// both selector-resolution passes short-circuit without touching the
			// graph and the request reaches queryContentStoreCoverage's graph
			// fallback (queryRepositoryGraphCoverageStats), which surfaces this
			// sentinel via RunSingle.
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-test/coverage", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
