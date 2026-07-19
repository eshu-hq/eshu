// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// This suite proves the auth-headerless-bypass hardening actually reaches the
// MCP HTTP transport (GET /sse, POST /mcp/message), reconciling with F-7
// (#5168, merged as 46ea6f58d): before this fix, wireAPI's transportAuth used
// the legacy AuthMiddlewareWithScopedTokensAndGovernanceAudit constructor,
// which derives dev-mode-open from the shared ESHU_API_KEY alone
// (token != ""). A scoped-token-file-only or OIDC-only deployment with no
// ESHU_API_KEY therefore still served headerless initialize/tools/list/ping
// requests AND established SSE sessions -- full tool-catalog enumeration --
// even though the same deployment's /api/v0/* routes were already closed by
// authedHandler's enforcement-aware constructor. This suite builds the
// transport middleware with buildTransportAuthMiddleware, the SAME function
// wiring.go now uses for BOTH authedHandler and mcpAuthWiring.transportAuth
// (see wiring.go's authSourceConfigured consolidation), and drives GET /sse
// and POST /mcp/message through mcp.Server.Handler exactly as RunHTTP would,
// rather than hand-building an isolated resolver or bypassing the real
// transport routing.

// buildMCPTransportServer assembles a real mcp.Server whose GET /sse and
// POST /mcp/message are protected by the SAME buildTransportAuthMiddleware
// construction wireAPI feeds into mcpAuthWiring.transportAuth: the real,
// always-wired Postgres identity resolver (backed by a never-matching stub
// store, exactly like buildMCPAuthHandler), the real
// scopedtoken.ChainResolvers(identity, oidc, file) chain, and the real
// authEnforcementConfigured predicate.
func buildMCPTransportServer(apiKey string, fileResolver, oidcResolver query.ScopedTokenResolver) *mcp.Server {
	identityResolver := scopedtoken.NewPostgresIdentityResolver(
		pgstatus.NewScopedAPITokenStore(stubExecQueryer{}),
	)
	authSourceConfigured := authEnforcementConfigured(apiKey, fileResolver, oidcResolver)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, oidcResolver, fileResolver)
	transportAuth := buildTransportAuthMiddleware(apiKey, scopedTokenResolver, nil, authSourceConfigured)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repos":[]}`))
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return mcp.NewServer(mux, logger, mcp.WithTransportAuth(transportAuth))
}

// TestMCPTransportAuthOIDCOnlyDeniesHeaderlessSSEAndMessage is THE
// reconciliation regression: an OIDC-only deployment (no ESHU_API_KEY) must
// deny a headerless GET /sse and a headerless POST /mcp/message
// initialize/tools/list/ping, not just headerless /api/v0/* reads. Pre-fix,
// every one of these returned 200 because the legacy constructor never saw
// the enforcement predicate.
func TestMCPTransportAuthOIDCOnlyDeniesHeaderlessSSEAndMessage(t *testing.T) {
	t.Parallel()

	oidc := &recordingScopedResolver{}
	server := buildMCPTransportServer("", nil, oidc)
	handler := server.Handler(nil)

	t.Run("GET /sse", func(t *testing.T) {
		if status := getSSERealStatus(t, handler, ""); status != http.StatusUnauthorized {
			t.Fatalf("headerless GET /sse status = %d, want 401", status)
		}
	})

	for _, method := range []string{"initialize", "tools/list", "ping"} {
		t.Run("POST /mcp/message "+method, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"}`
			req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("headerless POST /mcp/message %s status = %d, want 401; body = %s", method, rec.Code, rec.Body.String())
			}
		})
	}

	if oidc.called {
		t.Fatal("oidc resolver consulted for a headerless request; denial must precede resolution")
	}
}

// TestMCPTransportAuthScopedFileOnlyDeniesHeaderlessAdmitsToken proves the
// same closure for a scoped-token-file-only deployment (no ESHU_API_KEY,
// ESHU_SCOPED_TOKENS_FILE set): headerless GET /sse and POST /mcp/message are
// denied, and a request bearing the registered file token is still admitted
// -- the fix closes the headerless bypass without breaking legitimate
// scoped-token access.
func TestMCPTransportAuthScopedFileOnlyDeniesHeaderlessAdmitsToken(t *testing.T) {
	t.Parallel()

	const token = "file-registry-token-secret"
	fileResolver := writeScopedTokenFile(t, token)
	server := buildMCPTransportServer("", fileResolver, nil)
	handler := server.Handler(nil)

	headerless := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	))
	headerlessRec := httptest.NewRecorder()
	handler.ServeHTTP(headerlessRec, headerless)
	if headerlessRec.Code != http.StatusUnauthorized {
		t.Fatalf("headerless POST /mcp/message status = %d, want 401; body = %s", headerlessRec.Code, headerlessRec.Body.String())
	}

	if status := getSSERealStatus(t, handler, ""); status != http.StatusUnauthorized {
		t.Fatalf("headerless GET /sse status = %d, want 401", status)
	}

	authed := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	))
	authed.Header.Set("Authorization", "Bearer "+token)
	authedRec := httptest.NewRecorder()
	handler.ServeHTTP(authedRec, authed)
	if authedRec.Code != http.StatusOK {
		t.Fatalf("authenticated POST /mcp/message tools/list status = %d, want 200; body = %s", authedRec.Code, authedRec.Body.String())
	}

	sessionID := openSSESessionForTest(t, handler, token)
	if sessionID == "" {
		t.Fatal("authenticated GET /sse: empty sessionId, want a real session")
	}
}

// TestMCPTransportAuthDemoConfigServesHeaderlessOpen keeps the documented
// demo-shape (no explicit credential source) behavior intact: GET /sse and
// POST /mcp/message stay reachable headerless when NONE of the three
// enforcement knobs is configured, mirroring /api/v0/*'s dev-mode-open
// behavior and the local `eshu mcp start` / Compose zero-setup flow.
func TestMCPTransportAuthDemoConfigServesHeaderlessOpen(t *testing.T) {
	t.Parallel()

	server := buildMCPTransportServer("", nil, nil)
	handler := server.Handler(nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demo-config headerless POST /mcp/message status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	sessionID := openSSESessionForTest(t, handler, "")
	if sessionID == "" {
		t.Fatal("demo-config headerless GET /sse: empty sessionId, want a real session (headerless must stay open)")
	}
}

// getSSERealStatus issues a real GET /sse request over a live listener
// (httptest.NewServer) and returns the response status code, closing the
// connection immediately after headers arrive. A real listener is REQUIRED
// for every /sse assertion in this suite, not only the "admitted" ones:
// httptest.NewRecorder runs the handler synchronously in the calling
// goroutine, so if the handler ever incorrectly admits a headerless
// connection (the exact regression this suite guards against) the SSE
// keepalive loop blocks forever and the test hangs rather than failing. A
// real listener runs the handler in its own per-connection goroutine, so
// http.Client.Do returns as soon as response headers are flushed regardless
// of whether the server later holds the connection open.
func getSSERealStatus(t *testing.T, handler http.Handler, credential string) int {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sse", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// openSSESessionForTest opens a real GET /sse connection over a real
// listener (httptest.NewServer), reads the endpoint event, and returns the
// sessionId. When credential is non-empty it is sent as a Bearer
// Authorization header; an empty credential sends no header at all
// (exercising the headerless path). It closes the connection before
// returning so the test does not leak a goroutine. httptest.NewRecorder
// cannot be used here: the real SSE handler blocks on keepalives with no
// client-disconnect signal, so proving a session was actually established
// (not just that headers were written) requires a real client that can close
// its side of the connection.
func openSSESessionForTest(t *testing.T, handler http.Handler, credential string) string {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sse", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
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
	return strings.TrimPrefix(dataLine, "data: /mcp/message?sessionId=")
}
