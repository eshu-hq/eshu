// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCodeQualityInspectionMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/quality", bytes.NewBufferString(`{"check":"function_length"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleCodeQualityInspection(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestHandleSearchBundlesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/registry/bundles/search", bytes.NewBufferString(`{"query":"left-pad"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleSearchBundles(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestHandleRelationshipStoryRepoScopedOverridesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(`{"query_type":"overrides","repo_id":"repo-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationshipStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestCodeownersOwnershipListOwnershipMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeownersOwnershipHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listOwnership(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestResolveEntityWorkloadTypeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(`{"name":"api-node","type":"workload"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.resolveEntity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// canonicalHydrateContentStore is a minimal ContentStore double for
// TestResolveEntityCanonicalContentHydrationMapsGraphReadAvailabilityErrors: it
// serves the canonical content-entity lookup once (resolveCanonicalContentEntityID)
// and then returns nil on the second lookup performed by
// hydrateResolvedEntityRepoIdentityFromContent, so the workload repo-name
// backfill falls through to the graph read under test.
type canonicalHydrateContentStore struct {
	fakePortContentStore
	entity *EntityContent
	calls  int
}

func (s *canonicalHydrateContentStore) GetEntityContent(_ context.Context, _ string) (*EntityContent, error) {
	s.calls++
	if s.calls == 1 {
		return s.entity, nil
	}
	return nil, nil
}

func TestResolveEntityCanonicalContentHydrationMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			content := &canonicalHydrateContentStore{
				entity: &EntityContent{EntityID: "content-entity:workload-1", EntityType: "Workload", RepoID: "repo-1"},
			}
			handler := &EntityHandler{
				Content: content,
				Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				}},
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(`{"name":"content-entity:workload-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.resolveEntity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetWorkloadContextMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/context", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.SetPathValue("workload_id", "workload-1")
			rec := httptest.NewRecorder()

			handler.getWorkloadContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetWorkloadStoryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/story", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.SetPathValue("workload_id", "workload-1")
			rec := httptest.NewRecorder()

			handler.getWorkloadStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestInvestigateServiceMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/my-service", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.SetPathValue("service_name", "my-service")
			rec := httptest.NewRecorder()

			handler.investigateService(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
