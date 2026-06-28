// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingContractImpactGraph struct {
	runCalls []contractImpactRunCall
	runRows  [][]map[string]any
}

type contractImpactRunCall struct {
	cypher string
	params map[string]any
}

func (g *recordingContractImpactGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.runCalls = append(g.runCalls, contractImpactRunCall{cypher: cypher, params: params})
	if len(g.runRows) == 0 {
		return nil, nil
	}
	rows := g.runRows[0]
	g.runRows = g.runRows[1:]
	return rows, nil
}

func (g *recordingContractImpactGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestContractImpactRequiresSupportedProfile(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Neo4j: &recordingContractImpactGraph{}, Profile: ProfileLocalLightweight}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"family":"http","provider_repo_id":"repo-api"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	envelope := decodeContractImpactEnvelope(t, w)
	if envelope.Error == nil {
		t.Fatal("error envelope missing")
	}
	if got, want := envelope.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error code = %#v, want %#v", got, want)
	}
	if got, want := envelope.Error.Capability, contractImpactCapability; got != want {
		t.Fatalf("capability = %#v, want %#v", got, want)
	}
}

func TestContractImpactRejectsUnscopedRequest(t *testing.T) {
	t.Parallel()

	graph := &recordingContractImpactGraph{}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"family":"http"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if len(graph.runCalls) != 0 {
		t.Fatalf("graph calls = %d, want none for unscoped request", len(graph.runCalls))
	}
}

func TestContractImpactDefaultsFamilyToHTTP(t *testing.T) {
	t.Parallel()

	graph := &recordingContractImpactGraph{}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"provider_repo_id":"repo-api"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
	envelope := decodeContractImpactEnvelope(t, w)
	data := envelope.Data.(map[string]any)
	if got, want := data["family"], "http"; got != want {
		t.Fatalf("family = %#v, want %#v", got, want)
	}
}

func TestContractImpactReportsGRPCDeferralWithoutGraphRead(t *testing.T) {
	t.Parallel()

	graph := &recordingContractImpactGraph{}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"family":"grpc","provider_repo_id":"repo-api","service_name":"Catalog"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if len(graph.runCalls) != 0 {
		t.Fatalf("graph calls = %d, want none for explicitly deferred grpc", len(graph.runCalls))
	}
	envelope := decodeContractImpactEnvelope(t, w)
	data := envelope.Data.(map[string]any)
	families := data["families"].(map[string]any)
	grpc := families["grpc"].(map[string]any)
	if got, want := grpc["state"], "unsupported"; got != want {
		t.Fatalf("grpc state = %#v, want %#v", got, want)
	}
	if got, want := grpc["reason"], "protobuf_grpc_contract_projection_not_implemented"; got != want {
		t.Fatalf("grpc reason = %#v, want %#v", got, want)
	}
}

func TestContractImpactHTTPReturnsServiceUnavailableWithoutGraph(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"family":"http","provider_repo_id":"repo-api"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestContractImpactHTTPProvidersUseScopedEndpointQuery(t *testing.T) {
	t.Parallel()

	graph := &recordingContractImpactGraph{runRows: [][]map[string]any{{
		{
			"endpoint_id":      "endpoint:repo-api:/catalog",
			"provider_repo_id": "repo-api",
			"provider_repo":    "catalog-api",
			"path":             "/catalog",
			"methods":          []any{"GET"},
			"source_kinds":     []any{"openapi", "framework:fastapi"},
			"source_paths":     []any{"openapi.yaml", "app/routes.py"},
			"operation_ids":    []any{"listCatalog"},
			"workload_id":      "workload:catalog-api",
			"workload_name":    "catalog-api",
		},
	}}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/contracts",
		bytes.NewBufferString(`{"family":"http","provider_repo_id":"repo-api","route":"/catalog","method":"GET","limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
	call := graph.runCalls[0]
	for _, want := range []string{
		"MATCH (provider:Repository {id: $provider_repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)",
		"endpoint.path = $route",
		"$method_upper IN endpoint.methods",
		"ORDER BY endpoint.path, endpoint.id",
		"LIMIT $limit",
	} {
		if !strings.Contains(call.cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, call.cypher)
		}
	}
	if got, want := call.params["provider_repo_id"], "repo-api"; got != want {
		t.Fatalf("provider_repo_id param = %#v, want %#v", got, want)
	}
	if got, want := call.params["method"], "get"; got != want {
		t.Fatalf("method param = %#v, want lowercase %#v", got, want)
	}
	if got, want := call.params["method_upper"], "GET"; got != want {
		t.Fatalf("method_upper param = %#v, want uppercase %#v", got, want)
	}
	if got, want := call.params["limit"], 6; got != want {
		t.Fatalf("limit param = %#v, want limit+1 %#v", got, want)
	}

	envelope := decodeContractImpactEnvelope(t, w)
	if got, want := envelope.Truth.Capability, contractImpactCapability; got != want {
		t.Fatalf("truth capability = %#v, want %#v", got, want)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	providers := data["providers"].([]any)
	if got, want := len(providers), 1; got != want {
		t.Fatalf("providers = %d, want %d", got, want)
	}
	provider := providers[0].(map[string]any)
	if got, want := provider["contract_key"], "http:repo-api:/catalog:get"; got != want {
		t.Fatalf("contract_key = %#v, want %#v", got, want)
	}
	if got, want := provider["evidence_state"], "deterministic"; got != want {
		t.Fatalf("evidence_state = %#v, want %#v", got, want)
	}
}

func decodeContractImpactEnvelope(t *testing.T, w *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	return envelope
}
