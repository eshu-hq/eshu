// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// BenchmarkGetEntityContextResolution measures the per-request cost of
// (*Handler).GetEntityContext on the ways a request can end, driven through
// httptest against hermetic fakes (#7212). It exists to put a number on the
// resolution attribution the handler adds (resolved_by on the server span and
// on eshu_dp_entity_context_resolution_total); it is not a graph-backend
// benchmark, because the fake graph reader answers instantly and no real
// Cypher runs. The stmts/op metric is the number of graph statements the
// request sent, so a run shows that the statement count did not move.
//
// Each case runs twice: "bare" has no instruments and a request without a
// recording span (the cheapest the handler can run), and "instrumented" has
// real SDK instruments and a recording server span, which is the production
// shape. The benchmark uses only Handler fields that exist before #7212, so
// the same file runs against the earlier handler for a baseline.
func BenchmarkGetEntityContextResolution(b *testing.B) {
	contentHit := querycontract.EntityContent{
		EntityID: "ce-1", RepoID: "repo-1", RelativePath: "a.yaml",
		EntityType: "K8sResource", EntityName: "web",
	}
	cases := []struct {
		name     string
		backend  querycontract.GraphBackend
		entityID string
		// answer returns the graph row for the n-th (1-based) statement of a
		// request, or nil for a miss.
		answer     func(n int, cypher string) map[string]any
		content    querycontract.ContentStore
		wantStatus int
	}{
		{
			name:     "anchor_neo4j_fast_path",
			backend:  querycontract.GraphBackendNeo4j,
			entityID: "fn-1",
			answer: func(int, string) map[string]any {
				return benchRow("fn-1")
			},
			wantStatus: http.StatusOK,
		},
		{
			name:     "fallback_neo4j_second_statement",
			backend:  querycontract.GraphBackendNeo4j,
			entityID: "cr-1",
			answer: func(_ int, cypher string) map[string]any {
				if !strings.Contains(cypher, entityContextUnlabeledAnchor) {
					return nil
				}
				return benchRow("cr-1")
			},
			wantStatus: http.StatusOK,
		},
		{
			name:     "none_true_miss_neo4j",
			backend:  querycontract.GraphBackendNeo4j,
			entityID: "missing",
			answer:   func(int, string) map[string]any { return nil },
			content:  entityContextFakeContentStore{},
			// Every statement misses and the content store answers nothing.
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "content_answer_neo4j",
			backend:    querycontract.GraphBackendNeo4j,
			entityID:   "ce-1",
			answer:     func(int, string) map[string]any { return nil },
			content:    entityContextFakeContentStore{entity: &contentHit},
			wantStatus: http.StatusOK,
		},
		{
			name:     "anchor_nornicdb_loop_third_label",
			entityID: "fn-1",
			answer: func(n int, _ string) map[string]any {
				if n < 3 {
					return nil
				}
				return benchRow("fn-1")
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "none_true_miss_nornicdb_loop",
			entityID:   "missing",
			answer:     func(int, string) map[string]any { return nil },
			content:    entityContextFakeContentStore{},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		for _, instrumented := range []bool{false, true} {
			variant := "bare"
			if instrumented {
				variant = "instrumented"
			}
			b.Run(tc.name+"/"+variant, func(b *testing.B) {
				var statements, perRequest int
				handler := &Handler{
					GraphBackend: tc.backend,
					Neo4j: graph.FakeGraphReader{
						RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
							statements++
							perRequest++
							return tc.answer(perRequest, cypher), nil
						},
					},
					Content:              tc.content,
					ContentRelationships: scriptedContentRelationshipBuilder{},
					Profile:              querycontract.ProfileLocalAuthoritative,
				}
				var tracerProvider *sdktrace.TracerProvider
				if instrumented {
					meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
					b.Cleanup(func() { _ = meterProvider.Shutdown(context.Background()) })
					instruments, err := telemetry.NewInstruments(meterProvider.Meter("entity-context-bench"))
					if err != nil {
						b.Fatalf("NewInstruments() error = %v", err)
					}
					handler.Instruments = instruments
					tracerProvider = sdktrace.NewTracerProvider()
					b.Cleanup(func() { _ = tracerProvider.Shutdown(context.Background()) })
				}
				target := "/api/v0/entities/" + tc.entityID + "/context"

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					perRequest = 0
					ctx := context.Background()
					var end func()
					if tracerProvider != nil {
						spanCtx, span := tracerProvider.Tracer("entity-context-bench").Start(ctx, "GET /entities/{id}/context")
						ctx, end = spanCtx, func() { span.End() }
					}
					req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
					req.SetPathValue("entity_id", tc.entityID)
					rec := httptest.NewRecorder()
					handler.GetEntityContext(rec, req)
					if end != nil {
						end()
					}
					if rec.Code != tc.wantStatus {
						b.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(statements)/float64(b.N), "stmts/op")
			})
		}
	}
}

// benchRow is the minimal graph row the handler assembles into a 200.
func benchRow(id string) map[string]any {
	return map[string]any{
		"id":            id,
		"labels":        []any{"Function"},
		"name":          "Main",
		"relationships": []any{},
	}
}
