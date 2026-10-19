// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"fmt"
	"io"
	"net"
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

// TestRequestMiddlewareBodylessResponsesOmitTrailerAnnouncement proves a
// handler that writes no body never emits a Trailer announcement header
// (issue #7733 follow-up: the unconditional announcement crashed Node-based
// proxies such as the Vite dev server with ERR_HTTP_TRAILER_INVALID, failing
// the SSO E2E gate on #8482 while the test-only #8481 stayed green).
// Trailer headers are invalid on Content-Length responses, so the middleware
// must only announce when body bytes actually flow.
func TestRequestMiddlewareBodylessResponsesOmitTrailerAnnouncement(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/empty", func(_ http.ResponseWriter, _ *http.Request) {})
	mux.HandleFunc("GET /api/v0/accepted", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /api/v0/nocontent", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v0/explicit-ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(RequestMiddleware(mux))
	t.Cleanup(srv.Close)

	// Raw-wire reads: the Go client folds a Trailer announcement out of
	// resp.Header, which would hide exactly what crashes a Node proxy, so
	// bodyless routes are asserted on the raw header block instead.
	for _, target := range []string{"/api/v0/empty", "/api/v0/nocontent"} {
		raw := rawWireHeaders(t, "GET", srv.URL+target)
		if hasHeaderLine(raw, "Trailer:") {
			t.Fatalf("GET %s raw headers announce a trailer on a bodyless response:\n%s", target, raw)
		}
	}

	// Known residual, pinned: an explicit body-allowed status with no
	// following body still announces, as before this change. Holding that
	// send would re-emit a stored status and trip the repo's
	// no-direct-status-write guard (see server_status_guard_test.go), so it
	// stays as-is. No production instance exists: the tree's only such
	// shape, the MCP 202, serves POST /mcp/message outside this middleware.
	if raw := rawWireHeaders(t, "GET", srv.URL+"/api/v0/accepted"); !hasHeaderLine(raw, "Trailer:") {
		t.Fatalf("GET /api/v0/accepted raw headers lost the announcement; residual behavior changed:\n%s", raw)
	}

	// An explicit status followed by a body still delivers the trailer:
	// the announcement rides the header block flushed by the first Write.
	if total := serverTimingTrailer(t, srv.URL+"/api/v0/explicit-ok"); total < 0 {
		t.Fatal("explicit-200 route Server-Timing trailer missing (announcement lost)")
	}

	// HEAD responses carry no body, so they must not announce either.
	if raw := rawWireHeaders(t, "HEAD", srv.URL+"/api/v0/explicit-ok"); hasHeaderLine(raw, "Trailer:") {
		t.Fatalf("HEAD raw headers announce a trailer:\n%s", raw)
	}
}

// rawWireHeaders issues one raw HTTP/1.1 request over TCP and returns the
// response header block verbatim, so assertions see what a proxy sees.
func rawWireHeaders(t *testing.T, method, target string) string {
	t.Helper()
	u := strings.TrimPrefix(target, "http://")
	host, path, _ := strings.Cut(u, "/")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial %s error = %v", host, err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "%s /%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", method, path, host)
	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read raw response error = %v", err)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	return head
}

// hasHeaderLine reports whether the raw header block carries a line for the
// named header (prefix match up to the colon).
func hasHeaderLine(block, name string) bool {
	for line := range strings.Lines(block) {
		if strings.HasPrefix(line, name) {
			return true
		}
	}
	return false
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
