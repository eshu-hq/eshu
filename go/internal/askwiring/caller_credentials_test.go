// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package askwiring

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/engine"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/ask"
)

type callerEntrypointAdapter struct {
	calls  int
	leaked bool
}

func (a *callerEntrypointAdapter) complete(messages []provider.Message) provider.Completion {
	if strings.Contains(fmt.Sprint(messages), "browser-secret") || strings.Contains(fmt.Sprint(messages), "csrf-secret") || strings.Contains(fmt.Sprint(messages), "scoped-secret") {
		a.leaked = true
	}
	a.calls++
	if a.calls == 1 {
		return provider.Completion{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "find_code", Arguments: map[string]any{"query": "x", "repo_id": "repo-granted"}}}}
	}
	return provider.Completion{Text: "done"}
}

func (a *callerEntrypointAdapter) Complete(_ context.Context, messages []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	return a.complete(messages), nil
}

func (a *callerEntrypointAdapter) CompleteStream(_ context.Context, messages []provider.Message, _ []provider.Tool, _ func(provider.StreamEvent)) (provider.Completion, error) {
	return a.complete(messages), nil
}
func (*callerEntrypointAdapter) ModelID() string { return "fake-provider" }

type callerEntrypointSessionResolver struct{ calls int }

func (r *callerEntrypointSessionResolver) ResolveBrowserSession(_ context.Context, sessionHash, csrfHash string, requireCSRF bool, _ time.Time) (query.AuthContext, bool, error) {
	r.calls++
	if sessionHash != query.BrowserSessionSecretHash("browser-secret") || csrfHash != query.BrowserSessionSecretHash("csrf-secret") || !requireCSRF {
		return query.AuthContext{}, false, nil
	}
	return query.AuthContext{Mode: query.AuthModeBrowserSession, TenantID: "tenant", AllowedRepositoryIDs: []string{"repo-granted"}}, true, nil
}

type callerEntrypointScopedResolver struct{}

func (callerEntrypointScopedResolver) ResolveScopedToken(_ context.Context, token string) (query.AuthContext, bool, error) {
	if token != "scoped-secret" {
		return query.AuthContext{}, false, nil
	}
	return query.AuthContext{Mode: query.AuthModeScoped, TenantID: "tenant", AllowedRepositoryIDs: []string{"repo-granted"}}, true, nil
}

func TestEngineAskerReauthenticatesOriginalBrowserCredentials(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "json"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			adapter := &callerEntrypointAdapter{}
			resolver := &callerEntrypointSessionResolver{}
			leafCalls := 0
			leaf := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				leafCalls++
				ctx, ok := query.AuthContextFromContext(request.Context())
				if !ok || ctx.Mode != query.AuthModeBrowserSession || len(ctx.AllowedRepositoryIDs) != 1 || ctx.AllowedRepositoryIDs[0] != "repo-granted" {
					t.Fatalf("inner auth context=%+v found=%v", ctx, ok)
				}
				_, _ = io.WriteString(w, `{"data":{"matches":[]},"truth":null,"error":null}`)
			})
			authed := query.AuthMiddlewareWithBrowserSessionsAndScopedTokens("shared-admin", nil, resolver, leaf)
			runner := engine.NewMCPRunner(authed, "Bearer shared-admin", nil)
			eng, err := engine.New(adapter, runner, nil, engine.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			asker := &engineAsker{eng: eng}
			request := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
			request.AddCookie(&http.Cookie{Name: query.BrowserSessionCookieName, Value: "browser-secret"})
			request.Header.Set(query.BrowserSessionCSRFHeaderName, "csrf-secret")
			request = request.WithContext(query.ContextWithAuthContext(request.Context(), query.AuthContext{Mode: query.AuthModeBrowserSession}))
			if stream {
				_, err = asker.AskStream(request, "find code", func(ask.AskStreamEvent) {})
			} else {
				_, err = asker.Ask(request, "find code")
			}
			if err != nil || resolver.calls != 1 || leafCalls != 1 || adapter.leaked {
				t.Fatalf("Ask stream=%v err=%v session reauth=%d leaf=%d provider leak=%v", stream, err, resolver.calls, leafCalls, adapter.leaked)
			}
		})
	}
}

func TestEngineAskerReauthenticatesScopedBearer(t *testing.T) {
	adapter := &callerEntrypointAdapter{}
	leafCalls := 0
	leaf := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		leafCalls++
		ctx, ok := query.AuthContextFromContext(request.Context())
		if !ok || ctx.Mode != query.AuthModeScoped || len(ctx.AllowedRepositoryIDs) != 1 || ctx.AllowedRepositoryIDs[0] != "repo-granted" {
			t.Fatalf("inner scoped auth=%+v found=%v", ctx, ok)
		}
		_, _ = io.WriteString(w, `{"data":{"matches":[]},"truth":null,"error":null}`)
	})
	authed := query.AuthMiddlewareWithBrowserSessionsAndScopedTokens("shared-admin", callerEntrypointScopedResolver{}, nil, leaf)
	runner := engine.NewMCPRunner(authed, "Bearer shared-admin", nil)
	eng, err := engine.New(adapter, runner, nil, engine.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	request.Header.Set("Authorization", "Bearer scoped-secret")
	request = request.WithContext(query.ContextWithAuthContext(request.Context(), query.AuthContext{Mode: query.AuthModeScoped}))
	_, err = (&engineAsker{eng: eng}).Ask(request, "find code")
	if err != nil || leafCalls != 1 || adapter.leaked {
		t.Fatalf("scoped Ask err=%v leaf=%d provider leak=%v", err, leafCalls, adapter.leaked)
	}
}
