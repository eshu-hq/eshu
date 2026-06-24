// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestGitHubDependabotClientPaginatesWithCursorLinksWithinConfiguredBound(t *testing.T) {
	t.Parallel()

	var cursors stringRecorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("per_page"), "1"; got != want {
			t.Fatalf("per_page = %q, want %q", got, want)
		}
		if got := r.URL.Query().Get("page"); got != "" {
			t.Fatalf("page = %q, want empty because Dependabot alerts use cursor pagination", got)
		}
		if got, want := r.URL.Query().Get("state"), "open"; got != want {
			t.Fatalf("state = %q, want %q", got, want)
		}
		cursors.append(r.URL.Query().Get("after"))
		switch r.URL.Query().Get("after") {
		case "":
			w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&after=cursor-2>; rel="next"`)
		case "cursor-2":
			w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&after=cursor-3>; rel="next"`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("after"))
		}
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{Number: cursors.len(), State: "open"}})
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		AllowedRepositories:  []string{"example-org/example-repo"},
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 2)
	if err != nil {
		t.Fatalf("ListRepositoryAlertsPages() error = %v, want nil", err)
	}
	if got, want := len(result.Alerts), 2; got != want {
		t.Fatalf("len(Alerts) = %d, want %d", got, want)
	}
	if got, want := result.PagesFetched, 2; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true when a next page remains after max_pages")
	}
	if got, want := cursors.values(), []string{"", "cursor-2"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cursors = %#v, want %#v", got, want)
	}
}

func TestGitHubDependabotClientRequestsOpenAlertsSoFixedPagesDoNotHideOlderOpenAlerts(t *testing.T) {
	t.Parallel()

	var requestedStates stringRecorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("per_page"), "1"; got != want {
			t.Fatalf("per_page = %q, want %q", got, want)
		}
		requestedStates.append(r.URL.Query().Get("state"))
		if r.URL.Query().Get("state") == "open" {
			_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{Number: 17, State: "open"}})
			return
		}
		w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&after=older-open>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{
			Number:  3,
			State:   "fixed",
			FixedAt: "2026-05-20T12:00:00Z",
		}})
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		AllowedRepositories:  []string{"example-org/example-repo"},
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 1)
	if err != nil {
		t.Fatalf("ListRepositoryAlertsPages() error = %v, want nil", err)
	}
	if got, want := requestedStates.values(), []string{"open"}; !stringSlicesEqual(got, want) {
		t.Fatalf("requested states = %#v, want %#v", got, want)
	}
	if got, want := len(result.Alerts), 1; got != want {
		t.Fatalf("len(Alerts) = %d, want %d", got, want)
	}
	if got, want := result.Alerts[0].State, "open"; got != want {
		t.Fatalf("Alerts[0].State = %q, want %q", got, want)
	}
	if got, want := result.Alerts[0].Number, 17; got != want {
		t.Fatalf("Alerts[0].Number = %d, want older open alert %d", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false for complete open-alert page")
	}
}

type stringRecorder struct {
	mu          sync.Mutex
	valuesSlice []string
}

func (r *stringRecorder) append(value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.valuesSlice = append(r.valuesSlice, value)
}

func (r *stringRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.valuesSlice)
}

func (r *stringRecorder) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.valuesSlice...)
}

func TestGitHubDependabotClientReturnsRateLimitFailureMetadata(t *testing.T) {
	t.Parallel()

	reset := time.Date(2026, time.May, 25, 17, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1780000000")
		http.Error(w, "token secret-value and repository details must not escape", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		Token:               "token-value",
		AllowedRepositories: []string{"example-org/example-repo"},
	})
	_, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 2)
	if err == nil {
		t.Fatal("ListRepositoryAlertsPages() error = nil, want rate-limit error")
	}
	var failure GitHubDependabotError
	if !errors.As(err, &failure) {
		t.Fatalf("ListRepositoryAlertsPages() error = %T, want GitHubDependabotError", err)
	}
	if got, want := failure.FailureClass(), GitHubDependabotFailureRateLimited; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if got, want := failure.RateLimit.RetryAfter, 45*time.Second; got != want {
		t.Fatalf("RetryAfter = %v, want %v", got, want)
	}
	if got, want := failure.RateLimit.Remaining, 0; got != want {
		t.Fatalf("Remaining = %d, want %d", got, want)
	}
	if failure.RateLimit.Reset.Before(reset) {
		t.Fatalf("Reset = %s, want parsed epoch reset", failure.RateLimit.Reset)
	}
	if failure.Error() == "" || failure.Message == "" {
		t.Fatalf("GitHubDependabotError = %#v, want bounded message", failure)
	}
}

func TestGitHubDependabotClientWrapsStatusFailureWithSDKHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "token secret-value and repository details must not escape", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		Token:               "token-value",
		AllowedRepositories: []string{"example-org/example-repo"},
	})
	_, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 1)
	if err == nil {
		t.Fatal("ListRepositoryAlertsPages() error = nil, want provider error")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("ListRepositoryAlertsPages() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusForbidden; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
}

func TestGitHubDependabotClientParsesHTTPDateRetryAfter(t *testing.T) {
	t.Parallel()

	retryAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", retryAt.Format(http.TimeFormat))
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "provider rate limit", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		Token:               "token-value",
		AllowedRepositories: []string{"example-org/example-repo"},
	})
	_, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 1)
	if err == nil {
		t.Fatal("ListRepositoryAlertsPages() error = nil, want rate-limit error")
	}
	var failure GitHubDependabotError
	if !errors.As(err, &failure) {
		t.Fatalf("ListRepositoryAlertsPages() error = %T, want GitHubDependabotError", err)
	}
	if failure.RateLimit.RetryAfter < 90*time.Minute {
		t.Fatalf("RetryAfter = %v, want HTTP-date retry guidance", failure.RateLimit.RetryAfter)
	}
}

func TestGitHubDependabotClientDoesNotForwardTokenToCrossHostNextLink(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Link", `<http://127.0.0.1:1/dependabot/alerts?after=exfiltrate>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{Number: 1, State: "open"}})
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		AllowedRepositories:  []string{"example-org/example-repo"},
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 2)
	if err != nil {
		t.Fatalf("ListRepositoryAlertsPages() error = %v, want nil", err)
	}
	if got, want := requests, 1; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
	if got, want := result.PagesFetched, 1; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false when cross-host next link is ignored")
	}
}

func TestGitHubDependabotErrorClassifiesForbiddenWithoutRateLimitHeadersAsAuthDenied(t *testing.T) {
	t.Parallel()

	failure := GitHubDependabotError{StatusCode: http.StatusForbidden}
	if got, want := failure.FailureClass(), GitHubDependabotFailureAuthDenied; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if !failure.TerminalFailure() {
		t.Fatal("TerminalFailure = false, want true for auth-denied provider failures")
	}
}

func TestGitHubDependabotErrorDoesNotTreatResetHeaderAloneAsRateLimit(t *testing.T) {
	t.Parallel()

	failure := GitHubDependabotError{
		StatusCode: http.StatusForbidden,
		RateLimit: GitHubRateLimitInfo{
			Remaining:      42,
			RemainingKnown: true,
			Reset:          time.Date(2026, time.May, 25, 17, 0, 0, 0, time.UTC),
		},
	}
	if got, want := failure.FailureClass(), GitHubDependabotFailureAuthDenied; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

func serverURL(t *testing.T, r *http.Request) string {
	t.Helper()

	return "http://" + r.Host + r.URL.Path
}
