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

func TestGetWorkloadContextReturnsEnvelopeWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/context", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("workload_id", "workload-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	requireContextOverviewEnvelope(t, envelope)
	data := repositoryEnvelopeData(t, envelope)
	if got, want := data["id"], "workload-1"; got != want {
		t.Fatalf("data.id = %#v, want %#v", got, want)
	}
	if got, want := data["name"], "order-service"; got != want {
		t.Fatalf("data.name = %#v, want %#v", got, want)
	}
}

func TestGetWorkloadStoryReturnsEnvelopeWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("workload_id", "workload-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	requireContextOverviewEnvelope(t, envelope)
	data := repositoryEnvelopeData(t, envelope)
	if got, want := data["workload_id"], "workload-1"; got != want {
		t.Fatalf("data.workload_id = %#v, want %#v", got, want)
	}
	if story := StringVal(data, "story"); !strings.Contains(story, "Workload order-service") {
		t.Fatalf("data.story = %q, want workload narrative", story)
	}
}

func TestGetEntityContextReturnsEnvelopeWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "WHERE e.id = $entity_id") {
					t.Fatalf("RunSingle cypher = %q, want entity lookup", cypher)
				}
				if got, want := params["entity_id"], "entity-1"; got != want {
					t.Fatalf("entity_id param = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":            "entity-1",
					"labels":        []any{"Function"},
					"name":          "handler",
					"file_path":     "src/handler.go",
					"repo_id":       "repo-1",
					"repo_name":     "repo-1",
					"language":      "go",
					"start_line":    int64(12),
					"end_line":      int64(24),
					"relationships": []any{},
				}, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-1/context", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("entity_id", "entity-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	if envelope.Truth == nil {
		t.Fatal("truth is nil, want entity context truth envelope")
	}
	if got, want := envelope.Truth.Capability, "code_search.fuzzy_symbol"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisHybrid; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := repositoryEnvelopeData(t, envelope)
	if got, want := data["id"], "entity-1"; got != want {
		t.Fatalf("data.id = %#v, want %#v", got, want)
	}
}

func workloadEnvelopeGraphReader(workloadID, workloadName string) fakeWorkloadGraphReader {
	return fakeWorkloadGraphReader{
		runSingleByMatch: map[string]map[string]any{
			"MATCH (w:Workload)": {
				"id":        workloadID,
				"name":      workloadName,
				"kind":      "Deployment",
				"repo_id":   "repo-1",
				"repo_name": workloadName,
				"instances": []any{},
			},
		},
		runByMatch: map[string][]map[string]any{
			"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			"K8sResource OR":                      {},
			"fn.name IN":                          {},
		},
	}
}

func requireContextOverviewEnvelope(t *testing.T, envelope ResponseEnvelope) {
	t.Helper()

	if envelope.Truth == nil {
		t.Fatal("truth is nil, want context truth envelope")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisHybrid; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	if envelope.Error != nil {
		t.Fatalf("error = %#v, want nil", envelope.Error)
	}
}
