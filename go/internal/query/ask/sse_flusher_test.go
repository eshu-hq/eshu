// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// TestAskSSENoFlusherAnswersThroughTheServerFailureHelper is the #7674
// slice 14 regression for the SSE variant of POST /api/v0/ask on a response
// writer with no http.Flusher. The check runs before the handler commits the
// event-stream headers and before any Asker call, so the answer is a plain
// JSON 500. Before the slice it was written directly, with nothing on the
// request span. It now goes through tracing.ServerFailureEnvelope with the
// package sentinel errSSEFlushUnsupported: the span gets status Error and one
// exception event, and the body keeps its exact shape and fixed text.
//
// No error from a read reaches this step, and the sentinel is not a
// context.Canceled, so a canceled request still answers 500: there is no 499
// case here. The canceled case pins that.
func TestAskSSENoFlusherAnswersThroughTheServerFailureHelper(t *testing.T) {
	t.Parallel()

	for _, cancel := range []bool{false, true} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

		asker := &countingAsker{}
		h := &Handler{Asker: asker}
		ctx, span := provider.Tracer("ask-sse-test").Start(context.Background(), "request")
		ctx, stop := context.WithCancel(ctx)
		if cancel {
			stop()
		}
		nfw := &noFlushWriter{header: make(http.Header)}
		req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(`{"question":"anything"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		h.handleAsk(nfw, req)
		span.End()
		stop()

		label := map[bool]string{false: "live request", true: "canceled request"}[cancel]
		if nfw.code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", label, nfw.code)
		}
		if ct := nfw.header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s: Content-Type = %q, want application/json (event-stream headers must not be committed)", label, ct)
		}
		var body map[string]string
		if err := json.Unmarshal([]byte(nfw.buf.String()), &body); err != nil {
			t.Fatalf("%s: decode body: %v; body = %s", label, err, nfw.buf.String())
		}
		want := map[string]string{"error": "internal_error", "detail": sseFlushUnsupportedMessage}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("%s: body = %v, want %v", label, body, want)
		}
		if asker.calls != 0 {
			t.Errorf("%s: Asker called %d times before the flusher check, want 0", label, asker.calls)
		}

		ended := recorder.Ended()
		if len(ended) != 1 {
			t.Fatalf("%s: ended spans = %d, want 1", label, len(ended))
		}
		got := ended[0]
		if got.Status().Code != codes.Error || got.Status().Description != sseFlushUnsupportedMessage {
			t.Errorf("%s: span status = %v %q, want Error %q", label, got.Status().Code, got.Status().Description, sseFlushUnsupportedMessage)
		}
		exceptions, cancels := 0, 0
		for _, event := range got.Events() {
			switch event.Name {
			case "exception":
				exceptions++
			case tracing.ClientCanceledEvent:
				cancels++
			}
		}
		if exceptions != 1 || cancels != 0 {
			t.Errorf("%s: exception events = %d, cancel events = %d; want 1 and 0", label, exceptions, cancels)
		}
	}
}

// countingAsker counts every engine call, so a test can prove a step ran
// before the engine was reached.
type countingAsker struct{ calls int }

func (a *countingAsker) Ask(*http.Request, string) (AskAnswer, error) {
	a.calls++
	return AskAnswer{Prose: "ok", Narrated: true}, nil
}

func (a *countingAsker) AskStream(*http.Request, string, func(AskStreamEvent)) (AskAnswer, error) {
	a.calls++
	return AskAnswer{}, ErrNoStreaming
}
