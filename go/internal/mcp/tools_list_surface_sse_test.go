// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// sseTestServer serves the SSE and message routes the way RunHTTP mounts them.
func sseTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", s.handleSSE)
	mux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// openSSE opens GET /sse with query and returns the response and an event
// reader. The stream closes when the test ends.
func openSSE(t *testing.T, ts *httptest.Server, query string) (*http.Response, func() (event, data string)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/sse"+query, nil)
	if err != nil {
		t.Fatalf("build GET /sse: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return resp, func() (string, string) {
		var event, data string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && event != "":
				return event, data
			}
		}
		t.Fatalf("SSE stream ended before an event: %v", scanner.Err())
		return "", ""
	}
}

// TestSSEEndpointEventCarriesSurface proves GET /sse?surface=router puts the
// surface in the endpoint URL, so every POST the client follows carries it.
func TestSSEEndpointEventCarriesSurface(t *testing.T) {
	ts := sseTestServer(t, testServer())
	_, next := openSSE(t, ts, "?surface=router")
	event, data := next()
	if event != "endpoint" {
		t.Fatalf("first event = %q, want endpoint", event)
	}
	if !strings.HasPrefix(data, "/mcp/message?sessionId=") || !strings.HasSuffix(data, "&surface=router") {
		t.Fatalf("endpoint data = %q, want sessionId then &surface=router", data)
	}
}

// TestSSEEndpointEventOmitsSurfaceWhenUnset keeps the endpoint URL
// byte-compatible with main when the client sends no surface.
func TestSSEEndpointEventOmitsSurfaceWhenUnset(t *testing.T) {
	ts := sseTestServer(t, testServer())
	_, next := openSSE(t, ts, "")
	_, data := next()
	if strings.Contains(data, "surface") {
		t.Fatalf("endpoint data = %q, want no surface parameter", data)
	}
}

// TestSSEUnknownSurfaceIsRejected proves an unknown surface on GET /sse gets a
// 400 that names the allowed values, before any session is created.
func TestSSEUnknownSurfaceIsRejected(t *testing.T) {
	s := testServer()
	ts := sseTestServer(t, s)
	resp, err := http.Get(ts.URL + "/sse?surface=everything")
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	for _, want := range []string{"full", "router", "core_router"} {
		if !strings.Contains(string(buf[:n]), want) {
			t.Fatalf("400 body %q does not list %q", buf[:n], want)
		}
	}
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	if len(s.sessions) != 0 {
		t.Fatalf("%d session(s) registered for a rejected request", len(s.sessions))
	}
}

// TestSSELinkedPostUsesSessionSurface proves an SSE-linked POST with no
// surface of its own gets the session's router tools/list over the SSE
// channel (202 delivery), and that a POST surface still overrides it.
func TestSSELinkedPostUsesSessionSurface(t *testing.T) {
	ts := sseTestServer(t, testServer())
	_, next := openSSE(t, ts, "?surface=router")
	_, endpoint := next()
	// The client may follow the endpoint URL verbatim or only sessionId.
	cut := strings.Index(endpoint, "&surface=")
	if cut < 0 {
		t.Fatalf("endpoint %q carries no surface", endpoint)
	}
	sessionOnly := endpoint[:cut]

	post := func(path string) {
		t.Helper()
		resp, err := http.Post(ts.URL+path, "application/json",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST %s status = %d, want 202", path, resp.StatusCode)
		}
	}

	wantRouter := wantSurface(ReadOnlyTools(), router.SurfaceRouter)
	for _, path := range []string{endpoint, sessionOnly} {
		post(path)
		event, data := next()
		if event != "message" {
			t.Fatalf("event = %q, want message", event)
		}
		if got := listedNames(t, data); !reflect.DeepEqual(got, wantRouter) {
			t.Fatalf("POST %s listed %d tools, want router pair %v", path, len(got), wantRouter)
		}
	}

	post(sessionOnly + "&surface=full")
	_, data := next()
	if got, want := listedNames(t, data), toolNames(ReadOnlyTools()); !reflect.DeepEqual(got, want) {
		t.Fatalf("POST surface=full over session router listed %d tools, want %d", len(got), len(want))
	}
}

// TestSSESurfaceParamTrimsAndCanonicalizes proves GET /sse trims the value
// like the env setting and puts the canonical surface, not the raw one, in the
// endpoint event; a whitespace-only value means "absent".
func TestSSESurfaceParamTrimsAndCanonicalizes(t *testing.T) {
	ts := sseTestServer(t, testServer())

	_, next := openSSE(t, ts, "?surface=%20Core_Router%20")
	_, data := next()
	if !strings.HasSuffix(data, "&surface=core_router") || strings.Contains(data, "%20") || strings.Contains(data, "Core") {
		t.Fatalf("endpoint data = %q, want canonical &surface=core_router", data)
	}

	_, next = openSSE(t, ts, "?surface=%20%20")
	_, data = next()
	if strings.Contains(data, "surface") {
		t.Fatalf("whitespace-only surface produced endpoint %q, want none", data)
	}

	resp, err := http.Get(ts.URL + "/sse?surface=%20bogus%20")
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("padded unknown status = %d, want 400", resp.StatusCode)
	}
}
