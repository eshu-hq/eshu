// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetWorkloadContextReturnsResultLimitsAndPartialReasons proves the
// workload context HTTP route carries the additive result_limits drilldown
// block and an explicit partial_reasons slot alongside the truth envelope.
func TestGetWorkloadContextReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/workloads/workload-1/context", "workload_id", "workload-1")
	requireContextResultLimits(t, data, "get_workload_story", "/api/v0/workloads/workload-1/context")
}

// TestGetWorkloadStoryReturnsResultLimitsAndPartialReasons proves the workload
// story HTTP route carries the same hardened metadata, with the drilldown tool
// pointing back at the workload context route.
func TestGetWorkloadStoryReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/workloads/workload-1/story", "workload_id", "workload-1")
	requireContextResultLimits(t, data, "get_workload_context", "/api/v0/workloads/workload-1/context")
}

// TestGetEntityContextReturnsResultLimitsAndPartialReasons proves the entity
// context HTTP route carries the hardened result_limits drilldown block and an
// explicit partial_reasons slot alongside the truth envelope.
func TestGetEntityContextReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "WHERE e.id = $entity_id") {
					t.Fatalf("RunSingle cypher = %q, want entity lookup", cypher)
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

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	requireContextResultLimits(t, data, "get_relationship_evidence", "/api/v0/entities/entity-1/context")
}

func TestGetEntityContextHTTPDeterministicallyCapsRelationships(t *testing.T) {
	t.Parallel()

	relationships := make([]any, contextStoryItemLimit+10)
	for i := range relationships {
		index := len(relationships) - i - 1
		relationships[i] = map[string]any{
			"type":        "DEPENDS_ON",
			"target_name": fmt.Sprintf("target-%03d", index),
			"target_id":   fmt.Sprintf("repository:%03d", index),
		}
	}
	handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
		return map[string]any{
			"id":            "entity-1",
			"labels":        []any{"File"},
			"name":          "ci",
			"file_path":     ".github/workflows/ci.yml",
			"repo_id":       "repo-1",
			"relationships": relationships,
		}, nil
	}}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	first := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	second := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	for run, data := range []map[string]any{first, second} {
		rows := mapSliceValue(data, "relationships")
		if got, want := len(rows), contextStoryItemLimit; got != want {
			t.Fatalf("run %d relationship payload len = %d, want %d", run, got, want)
		}
		limits := mapValue(data, "result_limits")
		if got, want := IntVal(limits, "relationship_count"), contextStoryItemLimit+10; got != want {
			t.Fatalf("run %d relationship_count = %d, want %d", run, got, want)
		}
		if truncated, _ := limits["truncated"].(bool); !truncated {
			t.Fatalf("run %d result_limits.truncated = false, want true", run)
		}
		if got, want := StringVal(rows[0], "target_name"), "target-000"; got != want {
			t.Fatalf("run %d first target = %q, want %q", run, got, want)
		}
		if got, want := StringVal(rows[len(rows)-1], "target_name"), "target-049"; got != want {
			t.Fatalf("run %d last target = %q, want %q", run, got, want)
		}
	}
}

// TestWorkloadContextResultLimitsCapsFanoutInPlace proves the workload limits
// block caps each fan-out slice to the stated limit and only then reports
// truncated, so the payload never exceeds limit while claiming truncation.
func TestWorkloadContextResultLimitsCapsFanoutInPlace(t *testing.T) {
	t.Parallel()

	instances := make([]any, contextStoryItemLimit+10)
	for i := range instances {
		instances[i] = map[string]any{"id": i}
	}
	ctx := map[string]any{"instances": instances}

	limits := workloadContextResultLimits(ctx, "workload-1", "context")

	if got := len(mapSliceValue(ctx, "instances")); got != contextStoryItemLimit {
		t.Fatalf("instances payload len = %d, want capped to %d", got, contextStoryItemLimit)
	}
	if got, want := limits["instance_count"], contextStoryItemLimit+10; got != want {
		t.Fatalf("result_limits.instance_count = %v, want total %d", got, want)
	}
	if truncated, _ := limits["truncated"].(bool); !truncated {
		t.Fatal("result_limits.truncated = false, want true when fan-out exceeds the limit")
	}
}

// TestWorkloadContextResultLimitsNotTruncatedUnderLimit proves a workload whose
// fan-out is within the limit is not falsely marked truncated.
func TestWorkloadContextResultLimitsNotTruncatedUnderLimit(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"instances": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}}

	limits := workloadContextResultLimits(ctx, "workload-1", "context")

	if truncated, _ := limits["truncated"].(bool); truncated {
		t.Fatal("result_limits.truncated = true, want false when fan-out is within the limit")
	}
	if got := len(mapSliceValue(ctx, "instances")); got != 2 {
		t.Fatalf("instances payload len = %d, want 2 unchanged", got)
	}
}

func contextEnvelopeHTTPData(t *testing.T, mux *http.ServeMux, path, pathKey, pathValue string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue(pathKey, pathValue)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	if envelope.Truth == nil {
		t.Fatal("truth is nil, want context truth envelope")
	}
	return repositoryEnvelopeData(t, envelope)
}

func requireContextResultLimits(t *testing.T, data map[string]any, wantTool, wantPath string) {
	t.Helper()

	limits, ok := data["result_limits"].(map[string]any)
	if !ok || len(limits) == 0 {
		t.Fatalf("data.result_limits missing, want bounded drilldown block; data = %#v", data)
	}
	if limits["limit"] == nil {
		t.Fatal("result_limits.limit missing, want bounded limit")
	}
	if got, want := StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if _, ok := limits["truncated"].(bool); !ok {
		t.Fatalf("result_limits.truncated type = %T, want bool", limits["truncated"])
	}
	if got, want := StringVal(limits, "drilldown_tool"), wantTool; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := StringVal(limits, "context_path"), wantPath; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}
	if _, ok := data["partial_reasons"]; !ok {
		t.Fatal("data.partial_reasons missing, want explicit partial reason slot")
	}
}
