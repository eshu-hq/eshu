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

// TestTraceResourceToCodeMapsGraphReadAvailabilityErrors proves
// traceResourceToCode's start-anchor resolution (impact.go) maps the shared
// Neo4jReader sentinels to 503/504 instead of a generic 500.
func TestTraceResourceToCodeMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-resource-to-code", bytes.NewBufferString(`{"start":"repo-a"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestExplainDependencyPathMapsGraphReadAvailabilityErrors proves
// explainDependencyPath's source-anchor resolution (impact.go) maps the
// shared Neo4jReader sentinels to 503/504.
func TestExplainDependencyPathMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/explain-dependency-path", bytes.NewBufferString(`{"source":"a","target":"b"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestFindBlastRadiusMapsGraphReadAvailabilityErrors proves findBlastRadius
// (impact_blast_radius.go) maps the shared Neo4jReader sentinels to 503/504.
func TestFindBlastRadiusMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius", bytes.NewBufferString(`{"target":"repo-a","target_type":"repository"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestInvestigateChangeSurfaceMapsGraphReadAvailabilityErrors proves
// investigateChangeSurface (impact_change_surface_investigation.go) maps the
// shared Neo4jReader sentinels to 503/504.
func TestInvestigateChangeSurfaceMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(`{"service_name":"svc-a"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestFindChangeSurfaceMapsGraphReadAvailabilityErrors proves the legacy
// findChangeSurface handler (impact_change_surface_legacy.go) maps the shared
// Neo4jReader sentinels to 503/504.
func TestFindChangeSurfaceMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface", bytes.NewBufferString(`{"target":"svc-a"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestTraceDeploymentChainMapsGraphReadAvailabilityErrors proves
// traceDeploymentChain's workload-selector resolution
// (impact_trace_deployment.go / resolveTraceWorkloadSelector) maps the shared
// Neo4jReader sentinels to 503/504.
func TestTraceDeploymentChainMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				},
			}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", bytes.NewBufferString(`{"service_name":"svc-1"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestContractImpactMapsGraphReadAvailabilityErrors proves contractImpact
// (contract_impact.go) maps the shared Neo4jReader sentinels to 503/504.
func TestContractImpactMapsGraphReadAvailabilityErrors(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/contracts", bytes.NewBufferString(`{"family":"http","provider_repo_id":"repo-1"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestTraceExposurePathMapsGraphReadAvailabilityErrors proves
// traceExposurePath's bounded CALLS traversal (exposure_path.go) maps the
// shared Neo4jReader sentinels to 503/504. Reuses the exposureSourceContentStore
// and httpHandlerSourceEntity fixtures from exposure_path_handler_test.go so the
// source resolves and classifies before the graph read fails.
func TestTraceExposurePathMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{
				Profile: ProfileLocalAuthoritative,
				Content: exposureSourceContentStore{entity: httpHandlerSourceEntity()},
				Neo4j: fakeGraphReader{
					run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, test.err
					},
				},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-exposure-path", bytes.NewBufferString(`{"source_entity_id":"fn:handler"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
