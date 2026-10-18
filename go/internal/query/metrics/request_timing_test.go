// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRequestMiddlewareEmitsServerTimingTrailer proves RequestMiddleware
// exposes the per-request handler total as a Server-Timing trailer over the
// real HTTP wire (issue #7733: a slow first-touch POST /api/v0/code/search
// could not be attributed because no per-request timing exists).
//
// The trailer (not a response header) is deliberate: net/http flushes
// response headers on the handler's first body Write, so a header set after
// mux.ServeHTTP returns is silently dropped on the wire even though an
// httptest.ResponseRecorder would still show it. The test therefore drives
// the production middleware through httptest.NewServer with a real client
// and reads resp.Trailer after the body is fully consumed — a recorder-only
// assertion would pass for a header that production never sends.
func TestRequestMiddlewareEmitsServerTimingTrailer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/v0/slow", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("slow"))
	})
	srv := httptest.NewServer(RequestMiddleware(mux))
	t.Cleanup(srv.Close)

	// A fast route carries a well-formed total trailer.
	fast := serverTimingTrailer(t, srv.URL+"/api/v0/ok")
	if fast < 0 {
		t.Fatalf("fast route Server-Timing trailer missing or malformed (parsed %.1f)", fast)
	}

	// A slow route's trailer brackets the handler sleep, so the value is a
	// true total, not time-to-first-byte. The bound is half the sleep to
	// stay robust on a loaded host.
	slow := serverTimingTrailer(t, srv.URL+"/api/v0/slow")
	if slow < 50 {
		t.Fatalf("slow route total = %.1fms, want >= 50ms", slow)
	}
}

// TestRequestMiddlewareServerTimingTrailerSurvivesStreaming proves the
// trailer still arrives when the handler flushes early (the Ask SSE shape):
// trailers are announced before the delegate runs, so an early flush does
// not lose the total.
func TestRequestMiddlewareServerTimingTrailerSurvivesStreaming(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/stream", func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("chunk-one\n"))
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("chunk-two\n"))
	})
	srv := httptest.NewServer(RequestMiddleware(mux))
	t.Cleanup(srv.Close)

	total := serverTimingTrailer(t, srv.URL+"/api/v0/stream")
	if total < 50 {
		t.Fatalf("streamed route total = %.1fms, want >= 50ms", total)
	}
}

// serverTimingTrailer GETs target through the real wire, consumes the body,
// and parses the Server-Timing trailer's total stage in milliseconds.
// It returns -1 when the trailer is absent or malformed.
func serverTimingTrailer(t *testing.T, target string) float64 {
	t.Helper()
	resp, err := http.Get(target) //nolint:gosec,noctx // test-local server, no timeout needed
	if err != nil {
		t.Fatalf("GET %s error = %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read body error = %v", err)
	}
	raw := resp.Trailer.Get("Server-Timing")
	if raw == "" {
		return -1
	}
	const prefix = "total;dur="
	if !strings.HasPrefix(raw, prefix) {
		return -1
	}
	ms, err := strconv.ParseFloat(strings.TrimPrefix(raw, prefix), 64)
	if err != nil || ms < 0 {
		return -1
	}
	return ms
}
