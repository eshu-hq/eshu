// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeScopedTokenResolver resolves a fixed set of credential -> AuthContext
// pairs, mirroring a real scoped-token registry closely enough to exercise
// authenticatedTransportHandler and the SSE session-principal binding without
// standing up Postgres or a file-backed registry.
type fakeScopedTokenResolver struct {
	byCredential map[string]query.AuthContext
}

func (f *fakeScopedTokenResolver) ResolveScopedToken(_ context.Context, credential string) (query.AuthContext, bool, error) {
	auth, ok := f.byCredential[credential]
	return auth, ok, nil
}

// authedTestServer builds a Server whose GET /sse and POST /mcp/message
// transport endpoints require a bearer credential resolved by resolver,
// mirroring how cmd/mcp-server/wiring.go wires the SAME credential chain used
// for tools/call's internal dispatch (issue #5168).
func authedTestServer(t *testing.T, resolver query.ScopedTokenResolver) *Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"repos": []string{"test/repo"}})
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// A non-empty shared apiKey is required here even though these tests
	// authenticate via scoped tokens: query.AuthMiddleware's dev-mode bypass
	// triggers on shared-token emptiness alone for any headerless request,
	// regardless of whether a scoped-token resolver is also configured (see
	// go/internal/query/auth.go's authMiddlewareWithRoutePolicy). Using a
	// fixed non-matching shared token here mirrors a real deployment that has
	// closed the headerless-bypass hole.
	const unusedSharedAPIKey = "test-shared-key-never-sent-by-these-tests"
	transportAuth := func(next http.Handler) http.Handler {
		return query.AuthMiddlewareWithScopedTokensAndGovernanceAudit(unusedSharedAPIKey, resolver, next, nil)
	}
	return NewServer(mux, logger, WithTransportAuth(transportAuth))
}

func fullHTTPMux(s *Server) *http.ServeMux {
	return s.httpMux(nil)
}

// TestTransportAuth_UnauthenticatedMCPMessageDenied proves initialize,
// tools/list, and ping all require the same credential chain as tools/call
// once transport auth is configured, and that the denial response discloses
// nothing about the tool catalog or server identity (negative leakage).
func TestTransportAuth_UnauthenticatedMCPMessageDenied(t *testing.T) {
	resolver := &fakeScopedTokenResolver{byCredential: map[string]query.AuthContext{
		"token-a": {Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a", SubjectIDHash: "sub-a"},
	}}
	s := authedTestServer(t, resolver)
	mux := fullHTTPMux(s)

	methods := []string{"initialize", "tools/list", "ping"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"}`
			req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("method %s: status = %d, want 401", method, rec.Code)
			}
			leaked := rec.Body.String()
			for _, forbidden := range []string{"eshu-mcp-server", "tools", "serverInfo", "protocolVersion", "find_code", "list_indexed_repositories"} {
				if strings.Contains(leaked, forbidden) {
					t.Fatalf("method %s: 401 body leaked %q: %s", method, forbidden, leaked)
				}
			}
		})
	}
}

// TestTransportAuth_UnauthenticatedSSEDenied proves GET /sse requires the
// same credential before a session is established, and that the response
// does not disclose the endpoint event or session id.
func TestTransportAuth_UnauthenticatedSSEDenied(t *testing.T) {
	resolver := &fakeScopedTokenResolver{byCredential: map[string]query.AuthContext{
		"token-a": {Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a", SubjectIDHash: "sub-a"},
	}}
	s := authedTestServer(t, resolver)
	mux := fullHTTPMux(s)

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /sse status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "endpoint") || strings.Contains(rec.Body.String(), "sess_") {
		t.Fatalf("GET /sse 401 body leaked session/endpoint info: %s", rec.Body.String())
	}
	if len(s.sessions) != 0 {
		t.Fatalf("sessions = %d, want 0 -- unauthenticated GET /sse must not establish a session", len(s.sessions))
	}
}

// TestTransportAuth_ValidCredentialStillWorks is the regression guard: a
// correctly authenticated initialize/tools/list/ping/tools/call flow over
// POST /mcp/message must keep working exactly as before transport auth was
// added.
func TestTransportAuth_ValidCredentialStillWorks(t *testing.T) {
	resolver := &fakeScopedTokenResolver{byCredential: map[string]query.AuthContext{
		"token-a": {Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a", SubjectIDHash: "sub-a"},
	}}
	s := authedTestServer(t, resolver)
	mux := fullHTTPMux(s)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated tools/list status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["result"].(map[string]any); !ok {
		t.Fatalf("missing result: %s", rec.Body.String())
	}
}

// TestTransportAuth_SSESessionHijackRejected proves a credential resolving to
// a different principal than the one that opened an SSE session cannot post
// to that session's sessionId, and that a matching credential still can.
func TestTransportAuth_SSESessionHijackRejected(t *testing.T) {
	resolver := &fakeScopedTokenResolver{byCredential: map[string]query.AuthContext{
		"token-a": {Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a", SubjectIDHash: "sub-a"},
		"token-b": {Mode: query.AuthModeScoped, TenantID: "tenant-b", WorkspaceID: "ws-b", SubjectIDHash: "sub-b"},
	}}
	s := authedTestServer(t, resolver)
	mux := fullHTTPMux(s)

	sessionID := openSSESession(t, mux, "token-a")

	// Credential B (a different principal) must be rejected.
	hijackBody := `{"jsonrpc":"2.0","id":2,"method":"ping"}`
	hijackReq := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId="+sessionID, strings.NewReader(hijackBody))
	hijackReq.Header.Set("Authorization", "Bearer token-b")
	hijackRec := httptest.NewRecorder()
	mux.ServeHTTP(hijackRec, hijackReq)

	if hijackRec.Code != http.StatusForbidden {
		t.Fatalf("credential B posting to credential A's session: status = %d, want 403: %s", hijackRec.Code, hijackRec.Body.String())
	}

	// The rightful owner (credential A) must still be able to use its own session.
	ownerBody := `{"jsonrpc":"2.0","id":3,"method":"ping"}`
	ownerReq := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId="+sessionID, strings.NewReader(ownerBody))
	ownerReq.Header.Set("Authorization", "Bearer token-a")
	ownerRec := httptest.NewRecorder()
	mux.ServeHTTP(ownerRec, ownerReq)

	if ownerRec.Code != http.StatusAccepted {
		t.Fatalf("credential A posting to its own session: status = %d, want 202: %s", ownerRec.Code, ownerRec.Body.String())
	}
}

// openSSESession opens a real GET /sse connection authenticated as
// credential, reads the endpoint event, and returns the sessionId. It closes
// the connection before returning so the test does not leak a goroutine.
func openSSESession(t *testing.T, mux *http.ServeMux, credential string) string {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sse", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sse status = %d, want 200", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.After(2 * time.Second)
	var dataLine string
	for dataLine == "" {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for SSE endpoint event")
		default:
		}
		if !scanner.Scan() {
			t.Fatalf("SSE stream closed before endpoint event: %v", scanner.Err())
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data: /mcp/message?sessionId=") {
			dataLine = line
		}
	}
	sessionID := strings.TrimPrefix(dataLine, "data: /mcp/message?sessionId=")
	if sessionID == "" {
		t.Fatalf("empty sessionId parsed from endpoint event %q", dataLine)
	}
	return sessionID
}
