// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This regression stays in root because it drives root's AskHandler through
// the request-metrics middleware that moved to internal/query/metrics (#6642).

// TestAskSSE_StreamsThroughMetricsMiddleware is the end-to-end regression for
// issue #3381: POST /api/v0/ask with Accept: text/event-stream served behind
// RequestMetricsMiddleware must stream a 200 event stream, not a 500 "streaming
// not supported by this server configuration" error.
func TestAskSSE_StreamsThroughMetricsMiddleware(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{
		answer: AskAnswer{Prose: "streamed answer", Narrated: true},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/ask", h.handleAsk)
	handler := RequestMetricsMiddleware(mux)

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
