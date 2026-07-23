// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEntityMapResolveMapsGraphReadAvailabilityErrors proves entityMap's
// resolveEntityMapStart guard (entity_map.go) maps the shared Neo4jReader
// sentinels to 503/504 instead of a generic 500.
func TestEntityMapResolveMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/entity-map", bytes.NewBufferString(`{"from":"repo-1","from_type":"repository"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestEntityMapNeighborhoodMapsGraphReadAvailabilityErrors proves entityMap's
// entityMapNeighborhoodRows guard (entity_map.go, via entity_map_traversal.go)
// maps the shared Neo4jReader sentinels to 503/504 once the start anchor
// resolves, and that the existing traversal span error/attribute telemetry
// still runs ahead of the guard.
func TestEntityMapNeighborhoodMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "(start:") {
						return nil, test.err
					}
					return []map[string]any{{
						"id":              "repo-1",
						"name":            "repo-1",
						"labels":          []string{"Repository"},
						"repo_id":         "repo-1",
						"environment":     "",
						"anchor_label":    "Repository",
						"anchor_property": "id",
						"anchor_value":    "repo-1",
					}}, nil
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/entity-map", bytes.NewBufferString(`{"from":"repo-1","from_type":"repository"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestInvestigateResourceResolveMapsGraphReadAvailabilityErrors proves
// investigateResource's resolveResourceInvestigationTarget guard
// (impact_resource_investigation.go) maps the shared Neo4jReader sentinels to
// 503/504.
func TestInvestigateResourceResolveMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/resource-investigation", bytes.NewBufferString(`{"query":"foo"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestInvestigateResourceSectionsMapsGraphReadAvailabilityErrors proves
// investigateResource's loadResourceInvestigationSections guard
// (impact_resource_investigation.go, via impact_resource_investigation_reads.go)
// maps the shared Neo4jReader sentinels to 503/504 once the resource resolves,
// including when the failure surfaces through the parallel-section
// errors.Join path.
func TestInvestigateResourceSectionsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "WorkloadInstance)-[rel:USES]->") {
						return nil, test.err
					}
					if strings.Contains(cypher, "MATCH (n:CloudResource)") {
						return []map[string]any{{
							"id":             "res-1",
							"name":           "res-1",
							"labels":         []string{"CloudResource"},
							"resource_type":  "s3",
							"provider":       "aws",
							"environment":    "",
							"repo_id":        "",
							"config_path":    "",
							"source":         "",
							"resource_id":    "res-1",
							"arn":            "",
							"resource_kind":  "",
							"resource_class": "",
						}}, nil
					}
					return nil, nil
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/resource-investigation", bytes.NewBufferString(`{"query":"foo"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestDeveloperChangePlanMapsGraphReadAvailabilityErrors proves
// developerChangePlan's guard (developer_change_plan.go) maps the shared
// Neo4jReader sentinels to 503/504 when the shared preChangeImpactResponse
// path (impact_change_surface_investigation.go's resolveChangeSurfaceTarget)
// hits a graph-read failure, instead of falling through
// preChangeImpactErrorStatus's generic-500 default.
func TestDeveloperChangePlanMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/developer-change-plan", bytes.NewBufferString(`{"service_name":"svc-1"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPreChangeImpactMapsGraphReadAvailabilityErrors proves preChangeImpact's
// guard (prechange_impact.go) maps the shared Neo4jReader sentinels to
// 503/504 when the shared preChangeImpactResponse path
// (impact_change_surface_investigation.go's resolveChangeSurfaceTarget) hits a
// graph-read failure, instead of falling through preChangeImpactErrorStatus's
// generic-500 default.
func TestPreChangeImpactMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(`{"service_name":"svc-1"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
