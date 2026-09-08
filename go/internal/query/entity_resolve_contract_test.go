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

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestResolveEntityHonorsLimitAndReturnsEnvelope(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: querytestutil.FakeGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want parameterized bounded LIMIT", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "entity:one", "name": "handler", "labels": []any{"Function"}},
					{"id": "entity:two", "name": "handler", "labels": []any{"Function"}},
					{"id": "entity:three", "name": "handler", "labels": []any{"Function"}},
				}, nil
			},
		},
		Content: querytestutil.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repository:r_proof", Name: "proof"}}},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"handler","repo_id":"repository:r_proof","limit":2}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.ResolveEntity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	entities, ok := data["entities"].([]any)
	if !ok || len(entities) != 2 {
		t.Fatalf("entities = %#v, want two rows", data["entities"])
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "code_search.fuzzy_symbol" {
		t.Fatalf("truth = %#v, want fuzzy symbol truth", envelope.Truth)
	}
}

func TestResolveEntityWorkloadTypeFiltersBeforeLimit(t *testing.T) {
	t.Parallel()

	resolveQuerySeen := false
	handler := &EntityHandler{
		Neo4j: querytestutil.FakeGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)") {
					return []map[string]any{}, nil
				}
				if !strings.Contains(cypher, "MATCH (w:Workload)") {
					t.Fatalf("unexpected workload resolve query: %q", cypher)
				}
				resolveQuerySeen = true
				if !strings.Contains(cypher, "WHERE w.name = $name") {
					t.Fatalf("cypher = %q, want workload label and name before LIMIT", cypher)
				}
				return []map[string]any{
					{"id": "workload:api-node-boats", "name": "api-node-boats", "labels": []any{"Workload"}, "repo_name": "boats"},
				}, nil
			},
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"api-node-boats","type":"workload","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.ResolveEntity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !resolveQuerySeen {
		t.Fatal("workload resolve query was not executed")
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want workload resolution truth")
	}
	if got, want := envelope.Truth.Capability, "code_search.exact_symbol"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, querycontract.TruthBasisAuthoritativeGraph; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}
}

func TestResolveEntityWorkloadTypeDoesNotFallbackToContent(t *testing.T) {
	t.Parallel()

	content := &recordingEntityResolveContentStore{
		anyRepo: []querycontract.EntityContent{{EntityID: "content-entity:not-a-workload", EntityType: "Workload"}},
	}
	handler := &EntityHandler{
		Content: content,
		Neo4j: querytestutil.FakeGraphReader{
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"missing-workload","type":"workload","limit":10}`),
	)
	rec := httptest.NewRecorder()

	handler.ResolveEntity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got := content.anyRepoNameCalls; got != 0 {
		t.Fatalf("content fallback calls = %d, want 0 for graph-authoritative workload", got)
	}
}

func TestResolveEntityWorkloadEmptyGrantUsesAuthoritativeGraphTruth(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: querytestutil.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Fatal("empty scoped access must not query the graph")
			return nil, nil
		}},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"restricted-service","type":"workload","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode: queryauth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.ResolveEntity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want workload resolution truth")
	}
	if got, want := envelope.Truth.Capability, "code_search.exact_symbol"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, querycontract.TruthBasisAuthoritativeGraph; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}
}

func TestResolveEntityWorkloadWithoutGraphReportsUnavailable(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"missing-service","type":"workload","limit":10}`),
	)
	rec := httptest.NewRecorder()

	handler.ResolveEntity(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "authoritative graph workload resolution is unavailable") {
		t.Fatalf("body = %s, want precise graph-unavailable error", rec.Body.String())
	}
}
