// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestRunnerBindsImmutableBrowserCredentialsWithoutAdminFallback(t *testing.T) {
	var seenAuth, seenCookie, seenCSRF string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenCookie = r.Header.Get("Cookie")
		seenCSRF = r.Header.Get("X-Eshu-CSRF")
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Add("Cookie", "__Host-eshu_session=first; unrelated=ignore; eshu_session=second")
	outer.Header.Add("Cookie", "__Host-eshu_session=third; __Host-eshu_csrf=proof; eshu_csrf=other")
	outer.Header.Set("X-Eshu-CSRF", "original-proof")
	ctx := ContextWithCallerRequestCredentials(t.Context(), outer)
	outer.Header.Set("Authorization", "Bearer admin-after-capture")
	outer.Header.Set("Cookie", "__Host-eshu_session=mutated")
	outer.Header.Set("X-Eshu-CSRF", "mutated")
	var logs bytes.Buffer
	runner := NewMCPRunner(handler, "Bearer baked-admin", slog.New(slog.NewTextHandler(&logs, nil)))
	if _, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "r"}); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" || seenCSRF != "original-proof" || seenCookie != "__Host-eshu_session=first; eshu_session=second; __Host-eshu_session=third; __Host-eshu_csrf=proof; eshu_csrf=other" {
		t.Fatalf("captured headers auth=%q cookie=%q csrf=%q", seenAuth, seenCookie, seenCSRF)
	}
	for _, secret := range []string{"baked-admin", "original-proof", "first", "second", "third"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("credential leaked to logger: %s", secret)
		}
	}
}

func TestRunnerExplicitBlankHeaderDoesNotSelectAdmin(t *testing.T) {
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	runner := NewMCPRunner(handler, "Bearer baked-admin", nil)
	if _, err := runner.Run(ContextWithCallerAuthHeader(t.Context(), ""), "find_code", map[string]any{"query": "x", "repo_id": "r"}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("blank request credential escalated to %q", gotAuth)
	}
}

func TestRunnerAuthenticatedContextWithoutMatchingCredentialFailsClosed(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	runner := NewMCPRunner(handler, "Bearer baked-admin", nil)
	cases := []context.Context{
		query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeBrowserSession}),
		ContextWithCallerAuthHeader(query.ContextWithAuthContext(t.Context(), query.AuthContext{Mode: query.AuthModeScoped}), ""),
	}
	for _, ctx := range cases {
		if _, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "r"}); err == nil {
			t.Fatal("authenticated context without matching request credential accepted")
		}
	}
	if called {
		t.Fatal("inner handler called for missing credential")
	}
}

func TestRunnerBearerWinsOverCookieAndLegacyFallbackRemains(t *testing.T) {
	var got []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization")+"|"+r.Header.Get("Cookie"))
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	runner := NewMCPRunner(handler, "Bearer baked-admin", nil)
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer invalid-original")
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "cookie"})
	if _, err := runner.Run(ContextWithCallerRequestCredentials(t.Context(), outer), "find_code", map[string]any{"query": "x", "repo_id": "r"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(t.Context(), "find_code", map[string]any{"query": "x", "repo_id": "r"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Bearer invalid-original|__Host-eshu_session=cookie" || got[1] != "Bearer baked-admin|" {
		t.Fatalf("credential dispatch = %v", got)
	}
}

func TestRunnerRequestCredentialCaptureIsSafeForParallelCalls(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer caller")
	ctx := ContextWithCallerRequestCredentials(t.Context(), outer)
	var mu sync.Mutex
	var observed []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		observed = append(observed, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
	})
	runner := NewMCPRunner(handler, "Bearer baked-admin", nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "r"}); err != nil {
				t.Errorf("run: %v", err)
			}
		}()
	}
	wg.Wait()
	for _, auth := range observed {
		if auth != "Bearer caller" {
			t.Fatalf("parallel auth = %q", auth)
		}
	}
}

func TestCallerCredentialFormattingRedactsSecrets(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.Header.Set("Authorization", "Bearer secret-bearer")
	outer.Header.Set("X-Eshu-CSRF", "secret-csrf")
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "secret-session"})
	credentials, ok := callerCredentialsFromContext(ContextWithCallerRequestCredentials(t.Context(), outer))
	if !ok {
		t.Fatal("missing captured credentials")
	}
	formatted := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	for _, secret := range []string{"secret-bearer", "secret-csrf", "secret-session"} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("formatted credentials leaked %q", secret)
		}
	}
}

func TestRunnerBlankMarkedRequestPreservesOpenAndEnforcingPostures(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	ctx := ContextWithCallerRequestCredentials(t.Context(), outer)
	for _, tc := range []struct {
		name, sharedKey string
		wantLeaf        bool
	}{
		{"open", "", true},
		{"enforcing", "admin", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			leaf := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				_, _ = io.WriteString(w, `{"data":null,"truth":null,"error":null}`)
			})
			authed := query.AuthMiddleware(tc.sharedKey, leaf)
			runner := NewMCPRunner(authed, "Bearer admin", nil)
			result, err := runner.Run(ctx, "find_code", map[string]any{"query": "x", "repo_id": "r"})
			if called != tc.wantLeaf {
				t.Fatalf("leaf called=%v, want %v", called, tc.wantLeaf)
			}
			if tc.wantLeaf && err != nil || !tc.wantLeaf && !deniedRunnerResult(result, err) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestCallerCredentialHandlerClonesAndClearsGeneratedHeaders(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	outer.AddCookie(&http.Cookie{Name: "__Host-eshu_session", Value: "caller-session"})
	outer.Header.Set("X-Eshu-CSRF", "caller-proof")
	credentials, _ := callerCredentialsFromContext(ContextWithCallerRequestCredentials(t.Context(), outer))
	generated := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil)
	generated.Header.Set("Authorization", "Bearer stale-admin")
	generated.Header.Set("Cookie", "__Host-eshu_session=stale-session")
	generated.Header.Set("X-Eshu-CSRF", "stale-proof")
	var seenAuth, seenCookie, seenCSRF string
	adapter := callerCredentialHandler{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenCookie = r.Header.Get("Cookie")
		seenCSRF = r.Header.Get("X-Eshu-CSRF")
		w.WriteHeader(http.StatusOK)
	}), credentials: credentials}
	adapter.ServeHTTP(httptest.NewRecorder(), generated)
	if seenAuth != "" || seenCookie != "__Host-eshu_session=caller-session" || seenCSRF != "caller-proof" {
		t.Fatalf("adapter headers auth=%q cookie=%q csrf=%q", seenAuth, seenCookie, seenCSRF)
	}
	if generated.Header.Get("Authorization") != "Bearer stale-admin" || generated.Header.Get("Cookie") != "__Host-eshu_session=stale-session" || generated.Header.Get("X-Eshu-CSRF") != "stale-proof" {
		t.Fatal("adapter mutated generated request headers")
	}
}
