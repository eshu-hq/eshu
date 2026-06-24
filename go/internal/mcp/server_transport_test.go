// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleSSE_EndpointEvent(t *testing.T) {
	s := testServer()

	// Start test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(s.handleSSE))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// Read the first event (endpoint).
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	deadline := time.After(2 * time.Second)

	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for endpoint event")
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		lines = append(lines, line)
		// An empty line marks end of an SSE event.
		if line == "" && len(lines) > 1 {
			break
		}
	}

	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %v", len(lines), lines)
	}

	foundEndpoint := false
	for _, l := range lines {
		if strings.HasPrefix(l, "event: endpoint") {
			foundEndpoint = true
		}
	}
	if !foundEndpoint {
		t.Errorf("expected 'event: endpoint' line, got: %v", lines)
	}

	foundData := false
	for _, l := range lines {
		if strings.HasPrefix(l, "data: /mcp/message?sessionId=") {
			foundData = true
		}
	}
	if !foundData {
		t.Errorf("expected 'data: /mcp/message?sessionId=...' line, got: %v", lines)
	}
}

// Verify Health endpoint works through RunHTTP mux setup.
func TestHealth_ViaHTTPMux(t *testing.T) {
	s := testServer()

	// Replicate the mux setup from RunHTTP.
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	httpMux.HandleFunc("GET /sse", s.handleSSE)
	httpMux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)
	httpMux.Handle("/api/", s.handler)

	ts := httptest.NewServer(httpMux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", result["status"])
	}
}

// Verify API passthrough works.
func TestAPI_Passthrough(t *testing.T) {
	s := testServer()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)
	httpMux.Handle("/api/", s.handler)

	ts := httptest.NewServer(httpMux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v0/repositories")
	if err != nil {
		t.Fatalf("GET /api/v0/repositories: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body bytes.Buffer
	_, _ = io.Copy(&body, resp.Body)
	if !strings.Contains(body.String(), "test/repo") {
		t.Errorf("expected test/repo in response, got: %s", body.String())
	}
}

func TestHTTPMux_ComposesSharedAdminAndMCPRoutes(t *testing.T) {
	s := testServer()

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("admin-ok"))
	})

	httpMux := s.httpMux(adminMux)

	healthzReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzRec := httptest.NewRecorder()
	httpMux.ServeHTTP(healthzRec, healthzReq)
	if got, want := healthzRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}
	if got := strings.TrimSpace(healthzRec.Body.String()); got != "admin-ok" {
		t.Fatalf("GET /healthz body = %q, want %q", got, "admin-ok")
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	httpMux.ServeHTTP(healthRec, healthReq)
	if got, want := healthRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /health status = %d, want %d", got, want)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	apiRec := httptest.NewRecorder()
	httpMux.ServeHTTP(apiRec, apiReq)
	if got, want := apiRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/repositories status = %d, want %d", got, want)
	}
	if got := apiRec.Body.String(); !strings.Contains(got, "test/repo") {
		t.Fatalf("GET /api/v0/repositories body = %q, want test/repo", got)
	}
}
