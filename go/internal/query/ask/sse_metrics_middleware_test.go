// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/metrics"
)

// This regression lives in ask (not root) because it drives ask.Handler
// through the request-metrics middleware in internal/query/metrics (#6642);
// both homes are importable here, so no root alias is involved.

// TestAskSSE_StreamsThroughMetricsMiddleware is the end-to-end regression for
// issue #3381: POST /api/v0/ask with Accept: text/event-stream served behind
// metrics.RequestMiddleware must stream a 200 event stream, not a 500 "streaming
// not supported by this server configuration" error.
func TestAskSSE_StreamsThroughMetricsMiddleware(t *testing.T) {
	t.Parallel()

	h := &Handler{Asker: &fakeAsker{
		answer: AskAnswer{Prose: "streamed answer", Narrated: true},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/ask", h.handleAsk)
	handler := metrics.RequestMiddleware(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(`{"question":"stream check"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream; body=%s", ct, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: answer") {
		t.Fatalf("stream missing answer event; body=%s", rec.Body.String())
	}
}
