// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestInfraRelationshipsAnchorLoopSharesOneDeadlineAcrossLabels pins the
// #7006 review's P1 finding for the infra/relationships handler: the
// impactRelationshipAnchorLabels loop must derive ONE deadline before its
// first iteration and reuse it for every subsequent one, never let each
// RunSingle call start its own fresh Neo4jReader.runRead window. The
// request context here carries no deadline (matching production, which has
// no read/handler timeout for this route per the review), so any deadline
// the fake observes must come from the handler itself, and it must be the
// same instant on every call.
func TestInfraRelationshipsAnchorLoopSharesOneDeadlineAcrossLabels(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var deadlines []time.Time
	reader := fakeRepoGraphReader{
		runSingle: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("RunSingle ctx has no deadline; the anchor loop must derive one shared bounded-read deadline before it starts")
			}
			mu.Lock()
			deadlines = append(deadlines, deadline)
			mu.Unlock()
			return nil, nil // a genuine per-label miss
		},
	}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"workload:eshu"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if len(deadlines) < 2 {
		t.Fatalf("captured %d RunSingle deadlines, want at least 2 (the fake misses every label, so every candidate must be tried)", len(deadlines))
	}
	first := deadlines[0]
	for i, d := range deadlines[1:] {
		if !d.Equal(first) {
			t.Fatalf("call %d deadline = %s, want the same shared deadline as call 0 (%s) -- each call is getting its own fresh window instead of sharing one budget", i+1, d, first)
		}
	}
}

// TestInfraRelationshipsTranslatesASpentSharedBudgetToDeadlineResponse pins
// the review's other required behavior: when the shared budget is spent
// mid-loop, the handler must answer with the existing bounded-read deadline
// shape (504), never a silent not-found. A request-scoped short deadline
// simulates the shared budget already being exhausted without waiting out
// the real 10s production window; the fake returns the raw
// context.DeadlineExceeded exactly as Neo4jReader.runRead does when its
// parent context is already expired.
func TestInfraRelationshipsTranslatesASpentSharedBudgetToDeadlineResponse(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		runSingle: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"workload:eshu"}`))
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d (504 deadline shape); a spent shared budget must never resolve to a silent not-found or an unrelated error", got, want)
	}
}

// TestInfraRelationshipsSpanRecordsLabelsTried pins the review's telemetry
// ask: the request span must record how many labels the anchor loop tried,
// for both an eventual not-found (every label tried) and an early stop (a
// match found before the last label).
func TestInfraRelationshipsSpanRecordsLabelsTried(t *testing.T) {
	// Not t.Parallel(): this test swaps the package-global queryHandlerTracer
	// var for its duration (the same test seam
	// TestGraphEntityInventoryRecordsBoundedOutcomeTelemetry in
	// graph_entity_inventory_counts_test.go uses, also without t.Parallel()).
	// A parallel sibling test in this package that also drives a handler
	// through startQueryHandlerSpan would have its spans recorded into this
	// test's tracetest.SpanRecorder too, since the swap is process-global,
	// not per-goroutine -- that pollutes the "ended spans" count
	// nondeterministically depending on what else the test binary schedules
	// concurrently.

	cases := []struct {
		name      string
		matchOn   string // label to answer a match on; "" means miss every label
		wantTried int
	}{
		// A full miss tries every label, then the unlabeled fallback anchor,
		// which the attribute counts as one more anchor tried.
		{name: "full_miss", matchOn: "", wantTried: len(impactRelationshipAnchorLabels) + 1},
		{name: "early_match", matchOn: impactRelationshipAnchorLabels[1], wantTried: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previousTracer := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("infra-relationships-budget-test")
			t.Cleanup(func() { queryHandlerTracer = previousTracer })

			reader := fakeRepoGraphReader{
				runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
					if tc.matchOn != "" && strings.Contains(cypher, "MATCH (n:"+tc.matchOn+")") {
						return map[string]any{"id": "workload:eshu", "name": "eshu", "labels": []any{tc.matchOn}}, nil
					}
					return nil, nil
				},
			}
			handler := &InfraHandler{Neo4j: reader}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"workload:eshu"}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			var got int64
			var found bool
			for _, kv := range spans[0].Attributes() {
				if string(kv.Key) == "eshu.entity_anchor_labels_tried" {
					got = kv.Value.AsInt64()
					found = true
				}
			}
			if !found {
				t.Fatalf("span attributes = %#v, want eshu.entity_anchor_labels_tried present", spans[0].Attributes())
			}
			if got != int64(tc.wantTried) {
				t.Fatalf("eshu.entity_anchor_labels_tried = %d, want %d", got, tc.wantTried)
			}
		})
	}
}
