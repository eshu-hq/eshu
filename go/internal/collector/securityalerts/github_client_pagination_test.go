package securityalerts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGitHubDependabotClientPaginatesWithinConfiguredBound(t *testing.T) {
	t.Parallel()

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		if got, want := r.URL.Query().Get("per_page"), "1"; got != want {
			t.Fatalf("per_page = %q, want %q", got, want)
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&page=2>; rel="next"`)
		}
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{Number: len(pages), State: "open"}})
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
	if got, want := len(result.Alerts), 1; got != want {
		t.Fatalf("len(Alerts) = %d, want %d", got, want)
	}
	if got, want := result.PagesFetched, 1; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true when a next page remains after max_pages")
	}
	if got, want := pages, []string{"1"}; !stringSlicesEqual(got, want) {
		t.Fatalf("pages = %#v, want %#v", got, want)
	}
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
