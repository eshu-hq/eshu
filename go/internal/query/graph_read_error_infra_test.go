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

// graphReadSweepCase, graphReadSweepCases, and assertGraphReadSweepResponse
// are shared with the shared graph-read sweep helpers (same package): every
// batch's sweep test file drives its own handlers through the same
// sentinel-error table and bounded-contract assertion helper rather than
// redeclaring it per file.

func TestInfraSearchResourcesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"nginx"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.searchResources(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetEcosystemOverviewMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getEcosystemOverview(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetGraphSummaryPacketMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name+"/ecosystem", func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getGraphSummaryPacket(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
		t.Run(test.name+"/repo", func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}, runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{"repo_id":"repo-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getGraphSummaryPacket(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRelationshipsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"e1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRelationships(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListImagesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &ImageHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/images", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listImages(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRelationshipsCatalogMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/catalog", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRelationshipsCatalog(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestGetRelationshipEdgesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &InfraHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", strings.NewReader(`{"verb":"CALLS"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRelationshipEdges(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListTagHistoryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &TagHistoryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/images/tag-history?repository_id=oci-registry%3A%2F%2Fhost%2Fpath&tag=v1", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listTagHistory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestListDependenciesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &DependenciesHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/dependencies", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.listDependencies(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestInvestigateDeploymentConfigInfluenceMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name+"/trace_context", func(t *testing.T) {
			handler := &ImpactHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/deployment-config-influence", strings.NewReader(`{"service_name":"svc"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.investigateDeploymentConfigInfluence(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
