// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestImportDependencyInvestigationSpanCarriesTheCycleWalkSignals proves the
// operator-facing signals of a file_import_cycles read reach the handler span:
// how the walk ended, whether another page exists, and how many hops it
// examined. The steps examined are what let an operator see a request approach
// its step budget before it becomes a stop.
//
// It swaps the package span tracer for a recording one, so it runs serially and
// restores the tracer in a cleanup; parallel tests only start after every serial
// test, cleanup included, has finished.
func TestImportDependencyInvestigationSpanCarriesTheCycleWalkSignals(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("import-dependencies-span-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	edge := func(from, to string) map[string]any {
		return map[string]any{
			"repo_id": "repo-1", "repo_name": "repo",
			"source_path": "/repo/" + from + ".py", "source_file": from + ".py", "source_name": from + ".py",
			"language": "python", "target_module": to, "line_number": 1,
		}
	}
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{edge("a", "b"), edge("b", "a")}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"file_import_cycles","repo_id":"repo-1","limit":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("recorded %d spans, want the one handler span", len(ended))
	}
	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[kv.Key] = kv.Value
	}

	if got := attrs["eshu.import_dependencies.cycle_stop_reason"].AsString(); got != "none" {
		t.Errorf("cycle_stop_reason = %q, want none for a complete walk", got)
	}
	if got, ok := attrs["eshu.import_dependencies.has_more"]; !ok || got.AsBool() {
		t.Errorf("has_more = %v (present=%v), want present and false on the only page", got, ok)
	}
	steps, ok := attrs["eshu.import_dependencies.cycle_steps_examined"]
	if !ok {
		t.Fatalf("cycle_steps_examined attribute missing; the walk's cost must reach the span. attrs=%v", attrs)
	}
	if steps.AsInt64() <= 0 {
		t.Errorf("cycle_steps_examined = %d, want a positive count for a two-file cycle", steps.AsInt64())
	}
}
