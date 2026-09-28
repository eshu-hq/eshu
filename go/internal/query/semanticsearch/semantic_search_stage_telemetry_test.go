// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestSearchVectorReadyProbeSpanTracksOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    searchbench.Mode
		ready   SearchVectorReadyFreshness
		err     error
		outcome string
		calls   int
	}{
		{name: "keyword skips probe", mode: searchbench.ModeKeyword, calls: 0},
		{name: "missing watermark", mode: searchbench.ModeHybrid, ready: SearchVectorReadyFreshness{Signaled: true}, outcome: "missing", calls: 1},
		{name: "configured reader without signal", mode: searchbench.ModeHybrid, ready: SearchVectorReadyFreshness{}, outcome: "not_signaled", calls: 1},
		{name: "present watermark", mode: searchbench.ModeSemantic, ready: SearchVectorReadyFreshness{Signaled: true, Present: true}, outcome: "present", calls: 1},
		{name: "probe error", mode: searchbench.ModeHybrid, err: errors.New("private probe detail"), outcome: "error", calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previous := semanticSearchTracer
			semanticSearchTracer = provider.Tracer("semantic-search-stage-test")
			t.Cleanup(func() { semanticSearchTracer = previous })

			ctx, parent := semanticSearchTracer.Start(context.Background(), "request")
			ready := &fakeSearchVectorReadyReader{freshness: tc.ready, err: tc.err}
			handler := &SemanticSearchHandler{SearchVectorReady: ready}
			truth := handler.truthWithSearchVectorFreshness((&http.Request{}).WithContext(ctx), tc.mode)
			if tc.name == "configured reader without signal" && truth.Freshness.State != querycontract.FreshnessFresh {
				t.Fatalf("no-signal freshness = %q, want fresh", truth.Freshness.State)
			}
			parent.End()

			if ready.calls != tc.calls {
				t.Fatalf("watermark calls = %d, want %d", ready.calls, tc.calls)
			}
			spans := recorder.Ended()
			if tc.calls == 0 {
				if len(spans) != 1 {
					t.Fatalf("keyword spans = %d, want only request", len(spans))
				}
				return
			}
			if len(spans) != 2 {
				t.Fatalf("spans = %d, want probe and request", len(spans))
			}
			probe := spans[0]
			if probe.Name() != telemetry.SpanQuerySemanticSearchVectorReady {
				t.Fatalf("probe span = %q", probe.Name())
			}
			if probe.Parent().SpanID() != parent.SpanContext().SpanID() {
				t.Fatal("probe span is not a child of the request")
			}
			if got := semanticSearchSpanAttribute(probe.Attributes(), "search.vector_ready.outcome"); got != tc.outcome {
				t.Fatalf("probe outcome = %q, want %q", got, tc.outcome)
			}
			if tc.err != nil && probe.Status().Description == tc.err.Error() {
				t.Fatal("private probe error leaked into span status")
			}
		})
	}
}
