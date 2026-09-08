// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestGetServiceStoryReturnsEnvelopeDataWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":      "workload:service-edge-api",
					"name":    "service-edge-api",
					"kind":    "service",
					"repo_id": "repo-service-edge-api",
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.name = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data == nil {
		t.Fatal("envelope data is nil, want service dossier payload")
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "platform_impact.context_overview" {
		t.Fatalf("truth = %#v, want platform impact context truth", envelope.Truth)
	}
}

func TestGetServiceStoryReturnsEnvelopeErrorWhenServiceMissing(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/missing/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "missing")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data != nil {
		t.Fatalf("envelope data = %#v, want nil", envelope.Data)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("envelope error = %#v, want not_found", envelope.Error)
	}
}

func TestGetServiceStoryReturnsAmbiguousEnvelopeCandidates(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if got, want := params["service_name"], "checkout"; got != want {
					t.Fatalf("params[service_name] = %#v, want %q", got, want)
				}
				return []map[string]any{
					{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api", "environment": "prod"},
					{"id": "workload:checkout-worker", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-worker", "environment": "qa"},
				}, nil
			},
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				t.Fatal("ambiguous service story must not fetch or enrich a random workload")
				return nil, nil
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeAmbiguous {
		t.Fatalf("envelope error = %#v, want ambiguous", envelope.Error)
	}
	if envelope.Data != nil {
		t.Fatalf("envelope data = %#v, want nil on error", envelope.Data)
	}
	candidates, ok := envelope.Error.Details["candidates"].([]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want 2 candidates", envelope.Error.Details["candidates"])
	}
}

func TestGetServiceStoryRepoSelectorDisambiguatesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{ID: "repo-checkout-api", Name: "checkout-api"}},
		},
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
					return []map[string]any{{"repo_id": "repo-checkout-api", "repo_name": "checkout-api"}}, nil
				}
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if !strings.Contains(cypher, "w.repo_id = $repo_id") {
					t.Fatalf("service candidate query missing repo filter:\n%s", cypher)
				}
				if got, want := params["repo_id"], "repo-checkout-api"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?repo=checkout-api", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", envelope.Data)
	}
	identity := data["service_identity"].(map[string]any)
	if got, want := identity["repo_id"], "repo-checkout-api"; got != want {
		t.Fatalf("service_identity.repo_id = %#v, want %q", got, want)
	}
}

func TestGetServiceStoryEnvironmentSelectorDisambiguatesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if !strings.Contains(cypher, "WorkloadInstance") || !strings.Contains(cypher, "i.environment = $environment") {
					t.Fatalf("environment selector query missing WorkloadInstance environment filter:\n%s", cypher)
				}
				if got, want := params["environment"], "prod"; got != want {
					t.Fatalf("params[environment] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api", "environment": "prod"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"WorkloadInstance":                    {{"instance_id": "instance:checkout:prod", "environment": "prod"}},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?environment=prod", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetServiceStoryServiceIDSelectorUsesExactWorkload(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.id = $service_id") {
					return nil, nil
				}
				if got, want := params["service_id"], "workload:checkout-api"; got != want {
					t.Fatalf("params[service_id] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?service_id=workload%3Acheckout-api", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
