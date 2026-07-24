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

// TestGetRepositoryStoryMapsGraphReadAvailabilityErrors covers
// repository.go's getRepositoryStory repository_lookup RunSingle guard.
func TestGetRepositoryStoryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-1/story", nil)
			req.SetPathValue("repo_id", "repo-story-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestResolveEntityMapsGraphReadAvailabilityErrors covers entity.go's
// resolveEntity repository-anchored graph Run guard.
func TestResolveEntityMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", strings.NewReader(`{"name":"foo","repo_id":"repo-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.resolveEntity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestResolveEntityHydrateMapsGraphReadAvailabilityErrors covers entity.go's
// resolveEntity hydrateResolvedEntityRepoIdentity guard, reached once the
// initial graph Run succeeds with a Workload-labeled row still missing its
// repo identity.
func TestResolveEntityHydrateMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "UNWIND $entity_ids") {
					return nil, test.err
				}
				return []map[string]any{{
					"id":        "entity-1",
					"labels":    []string{"Workload"},
					"name":      "foo",
					"repo_id":   "",
					"repo_name": "",
				}}, nil
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", strings.NewReader(`{"name":"foo","repo_id":"repo-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.resolveEntity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetEntityContextMapsGraphReadAvailabilityErrors covers entity.go's
// getEntityContext main RunSingle guard.
func TestGetEntityContextMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-1/context", nil)
			req.SetPathValue("entity_id", "entity-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getEntityContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetEntityContextHydrateMapsGraphReadAvailabilityErrors covers entity.go's
// getEntityContext hydrateResolvedEntityRepoIdentity guard, reached once the
// main RunSingle succeeds with a Workload-labeled row still missing its repo
// identity.
func TestGetEntityContextHydrateMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{
						"id":     "entity-1",
						"labels": []string{"Workload"},
						"name":   "foo",
					}, nil
				},
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-1/context", nil)
			req.SetPathValue("entity_id", "entity-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getEntityContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetServiceContextMapsGraphReadAvailabilityErrors covers entity.go's
// getServiceContext fetchServiceWorkloadContext guard.
func TestGetServiceContextMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/services/svc-1/context", nil)
			req.SetPathValue("service_name", "svc-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getServiceContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
