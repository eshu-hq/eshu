// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type askSessionResolver struct {
	mu          sync.Mutex
	called      int
	valid       bool
	session     string
	csrf        string
	requireCSRF bool
}

func (r *askSessionResolver) ResolveBrowserSession(_ context.Context, sessionHash, csrfHash string, requireCSRF bool, _ time.Time) (query.AuthContext, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called++
	r.session = sessionHash
	r.csrf = csrfHash
	r.requireCSRF = requireCSRF
	if !r.valid {
		return query.AuthContext{}, false, nil
	}
	if requireCSRF && csrfHash != query.BrowserSessionSecretHash("proof") {
		return query.AuthContext{}, false, query.ErrBrowserSessionCSRFInvalid
	}
	return query.AuthContext{Mode: query.AuthModeBrowserSession, AllowedRepositoryIDs: []string{"repo"}}, true, nil
}

func deniedRunnerResult(result RunResult, err error) bool {
	return err != nil || result.Envelope != nil && result.Envelope.Error != nil
}

func TestRunnerBrowserSessionReauthAndRevocation(t *testing.T) {
	resolver := &askSessionResolver{valid: true}
	leafCalls := 0
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leafCalls++
		auth, ok := query.AuthContextFromContext(r.Context())
		if !ok || auth.Mode != query.AuthModeBrowserSession {
			t.Fatalf("inner auth=%+v, ok=%v", auth, ok)
		}
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	wrapped := query.AuthMiddlewareWithBrowserSessionsAndScopedTokens("admin", nil, resolver, leaf)
	runner := NewMCPRunner(wrapped, "Bearer admin", nil)
	for _, tc := range []struct {
		name, cookie, selected string
	}{
		{"secure", "__Host-eshu_session=secure", "secure"},
		{"insecure", "eshu_session=insecure", "insecure"},
		{"secure-precedence", "eshu_session=other; __Host-eshu_session=secure", "secure"},
		{"duplicate-first", "__Host-eshu_session=first; __Host-eshu_session=second", "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
			outer.Header.Set("Cookie", tc.cookie)
			outer.Header.Set("X-Eshu-CSRF", "proof")
			ctx := ContextWithCallerRequestCredentials(query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeBrowserSession}), outer)
			if _, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "repo"}); err != nil {
				t.Fatal(err)
			}
			resolver.mu.Lock()
			gotSession, gotCSRF, required := resolver.session, resolver.csrf, resolver.requireCSRF
			resolver.mu.Unlock()
			if gotSession != query.BrowserSessionSecretHash(tc.selected) || gotCSRF != query.BrowserSessionSecretHash("proof") || !required {
				t.Fatalf("resolved session=%q csrf=%q required=%v", gotSession, gotCSRF, required)
			}
		})
	}
	if leafCalls != 4 {
		t.Fatalf("leaf calls=%d, want 4", leafCalls)
	}
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "secure"})
	outer.Header.Set("X-Eshu-CSRF", "proof")
	ctx := ContextWithCallerRequestCredentials(query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeBrowserSession}), outer)
	resolver.mu.Lock()
	resolver.valid = false // Revoked after Ask entry, before its next inner tool.
	resolver.mu.Unlock()
	if result, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "repo"}); !deniedRunnerResult(result, err) {
		t.Fatal("revoked browser session returned success")
	}
	if leafCalls != 4 {
		t.Fatalf("revoked session reached leaf: calls=%d", leafCalls)
	}
}

func TestRunnerBrowserInnerPostRequiresOriginalCSRF(t *testing.T) {
	resolver := &askSessionResolver{valid: true}
	leafCalls := 0
	wrapped := query.AuthMiddlewareWithBrowserSessionsAndScopedTokens("admin", nil, resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leafCalls++
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	}))
	runner := NewMCPRunner(wrapped, "Bearer admin", nil)
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "secure"})
	ctx := ContextWithCallerRequestCredentials(query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeBrowserSession}), outer)
	if result, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "repo"}); !deniedRunnerResult(result, err) {
		t.Fatal("missing original CSRF admitted inner POST")
	}
	if leafCalls != 0 {
		t.Fatalf("missing CSRF reached leaf: %d", leafCalls)
	}
}

func TestRunnerInvalidBearerDoesNotRetryCookieOrAdmin(t *testing.T) {
	resolver := &askSessionResolver{valid: true}
	leafCalls := 0
	wrapped := query.AuthMiddlewareWithBrowserSessionsAndScopedTokens("admin", nil, resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leafCalls++
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	}))
	runner := NewMCPRunner(wrapped, "Bearer admin", nil)
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer invalid")
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "secure"})
	ctx := ContextWithCallerRequestCredentials(query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeShared}), outer)
	if result, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "repo"}); !deniedRunnerResult(result, err) {
		t.Fatal("invalid bearer retried with cookie or admin")
	}
	if leafCalls != 0 || resolver.called != 0 {
		t.Fatalf("invalid bearer reached leaf=%d or cookie resolver=%d", leafCalls, resolver.called)
	}
}

func TestRunnerCanceledRequestDoesNotDispatch(t *testing.T) {
	called := false
	runner := NewMCPRunner(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), "Bearer admin", nil)
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer caller")
	parent, cancel := context.WithCancel(t.Context())
	ctx := ContextWithCallerRequestCredentials(parent, outer)
	cancel()
	if _, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "repo"}); err == nil || called {
		t.Fatalf("canceled inner call error=%v leaf=%v", err, called)
	}
}
