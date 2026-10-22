// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/github/app"
)

// appAuthTestServer serves Dependabot alerts with a fixed status and records
// the Bearer tokens it saw, so tests prove which credential each call used.
func appAuthTestServer(t *testing.T, status int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{})
			return
		}
		http.Error(w, "{}", status)
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func appAuthClient(t *testing.T, server *httptest.Server, provider func(context.Context) (string, error)) GitHubDependabotClient {
	t.Helper()
	return NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		AllowedRepositories: []string{"example-org/example-repo"},
		TokenProvider:       provider,
		AppAuth:             true,
	})
}

func staticAuthClient(server *httptest.Server) GitHubDependabotClient {
	return NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		Token:               "static-token",
		AllowedRepositories: []string{"example-org/example-repo"},
	})
}

// TestAppAuth401IsRetryableWhileStatic401IsDenied pins the deliberate
// divergence: an alert-read 401 under GitHub App auth is retryable (the
// hourly installation token likely expired), while the static 401 stays a
// terminal auth denial.
func TestAppAuth401IsRetryableWhileStatic401IsDenied(t *testing.T) {
	t.Parallel()

	server, _ := appAuthTestServer(t, http.StatusUnauthorized)

	staticErr := mustAlertPagesError(t, staticAuthClient(server), "example-org/example-repo")
	if staticErr.FailureClass() != GitHubDependabotFailureAuthDenied || !staticErr.TerminalFailure() {
		t.Fatalf("static 401: class=%q terminal=%v, want auth_denied terminal",
			staticErr.FailureClass(), staticErr.TerminalFailure())
	}

	app := appAuthClient(t, server, func(context.Context) (string, error) { return "minted-token", nil })
	appErr := mustAlertPagesError(t, app, "example-org/example-repo")
	if appErr.FailureClass() != GitHubDependabotFailureRetryable || appErr.TerminalFailure() {
		t.Fatalf("app 401: class=%q terminal=%v, want retryable non-terminal",
			appErr.FailureClass(), appErr.TerminalFailure())
	}
}

// TestAppAuth403StaysDenied pins that only the 401 diverges: a 403 is a
// sanitized terminal denial in both modes.
func TestAppAuth403StaysDenied(t *testing.T) {
	t.Parallel()

	server, _ := appAuthTestServer(t, http.StatusForbidden)

	staticErr := mustAlertPagesError(t, staticAuthClient(server), "example-org/example-repo")
	app := appAuthClient(t, server, func(context.Context) (string, error) { return "minted-token", nil })
	appErr := mustAlertPagesError(t, app, "example-org/example-repo")
	for name, err := range map[string]GitHubDependabotError{"static": staticErr, "app": appErr} {
		if err.FailureClass() != GitHubDependabotFailureAuthDenied || !err.TerminalFailure() {
			t.Fatalf("%s 403: class=%q terminal=%v, want auth_denied terminal",
				name, err.FailureClass(), err.TerminalFailure())
		}
		if strings.Contains(err.Error(), "minted-token") || strings.Contains(err.Error(), "static-token") {
			t.Fatalf("%s 403 error carries a credential: %v", name, err)
		}
	}
}

// TestTokenProviderMintsOncePerClaim pins one mint per claim, not per page:
// a two-page claim calls the provider exactly once and sends the minted
// token on both pages.
func TestTokenProviderMintsOncePerClaim(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int64
	var mu sync.Mutex
	var seen []string
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		alerts := []GitHubDependabotAlert{}
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", `<`+serverURL+`/repos/example-org/example-repo/dependabot/alerts?page=2>; rel="next"`)
		}
		_ = json.NewEncoder(w).Encode(alerts)
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	app := appAuthClient(t, server, func(context.Context) (string, error) {
		providerCalls.Add(1)
		return "minted-per-claim", nil
	})
	if _, err := app.ListRepositoryAlertsPages(context.Background(), "example-org/example-repo", 2); err != nil {
		t.Fatalf("ListRepositoryAlertsPages() error = %v, want nil", err)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 per claim", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("alert page requests = %d, want 2", len(seen))
	}
	for _, header := range seen {
		if header != "Bearer minted-per-claim" {
			t.Fatalf("authorization header = %q, want the minted token", header)
		}
	}
}

// TestMintFailuresMapToTerminalOrRetryable pins the mint failure mapping: a
// 404 or bad key is a terminal auth denial, while an outlasted transient or
// rate limit stays retryable.
func TestMintFailuresMapToTerminalOrRetryable(t *testing.T) {
	t.Parallel()

	server, _ := appAuthTestServer(t, http.StatusOK)
	tests := []struct {
		name         string
		provider     func(context.Context) (string, error)
		wantClass    string
		wantTerminal bool
	}{
		{
			name:         "installation not found",
			provider:     func(context.Context) (string, error) { return "", &app.NotFoundError{} },
			wantClass:    GitHubDependabotFailureAuthDenied,
			wantTerminal: true,
		},
		{
			name:         "denied credentials",
			provider:     func(context.Context) (string, error) { return "", &app.AuthDeniedError{StatusCode: 401} },
			wantClass:    GitHubDependabotFailureAuthDenied,
			wantTerminal: true,
		},
		{
			name:         "bad private key",
			provider:     func(context.Context) (string, error) { return "", app.NewConfigError("") },
			wantClass:    GitHubDependabotFailureAuthDenied,
			wantTerminal: true,
		},
		{
			name: "outlasted transient",
			provider: func(context.Context) (string, error) {
				return "", app.NewRetryableError(app.ClassServerError, 4, 0, errors.New("boom"))
			},
			wantClass:    GitHubDependabotFailureRetryable,
			wantTerminal: false,
		},
		{
			name: "deferred rate limit",
			provider: func(context.Context) (string, error) {
				return "", app.NewRetryableError(app.ClassRateLimited, 1, 0, errors.New("boom"))
			},
			wantClass:    GitHubDependabotFailureRateLimited,
			wantTerminal: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := appAuthClient(t, server, tt.provider)
			err := mustAlertPagesError(t, app, "example-org/example-repo")
			if err.FailureClass() != tt.wantClass || err.TerminalFailure() != tt.wantTerminal {
				t.Fatalf("class=%q terminal=%v, want %q terminal=%v",
					err.FailureClass(), err.TerminalFailure(), tt.wantClass, tt.wantTerminal)
			}
		})
	}
}

func mustAlertPagesError(t *testing.T, client GitHubDependabotClient, repository string) GitHubDependabotError {
	t.Helper()
	_, err := client.ListRepositoryAlertsPages(context.Background(), repository, 1)
	if err == nil {
		t.Fatal("ListRepositoryAlertsPages() error = nil, want a failure")
	}
	var ghErr GitHubDependabotError
	if !errors.As(err, &ghErr) {
		t.Fatalf("error type = %T, want GitHubDependabotError", err)
	}
	return ghErr
}
