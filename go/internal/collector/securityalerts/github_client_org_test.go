// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestGitHubDependabotClientListsOrganizationAlertsAcrossCursorPages(t *testing.T) {
	t.Parallel()

	var cursors stringRecorder
	var paths stringRecorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths.append(r.URL.Path)
		if got, want := r.URL.Path, "/orgs/example-org/dependabot/alerts"; got != want {
			t.Fatalf("path = %q, want org endpoint %q", got, want)
		}
		if got, want := r.URL.Query().Get("per_page"), "1"; got != want {
			t.Fatalf("per_page = %q, want %q", got, want)
		}
		if got := r.URL.Query().Get("page"); got != "" {
			t.Fatalf("page = %q, want empty because org Dependabot alerts use cursor pagination", got)
		}
		if got, want := r.URL.Query().Get("state"), "open"; got != want {
			t.Fatalf("state = %q, want %q", got, want)
		}
		cursors.append(r.URL.Query().Get("after"))
		switch r.URL.Query().Get("after") {
		case "":
			w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&after=org-cursor-2>; rel="next"`)
			_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{
				Number:     1,
				State:      "open",
				Repository: GitHubDependabotRepository{FullName: "example-org/alpha-repo", Name: "alpha-repo"},
			}})
		case "org-cursor-2":
			_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{
				Number:     2,
				State:      "open",
				Repository: GitHubDependabotRepository{FullName: "example-org/beta-repo", Name: "beta-repo"},
			}})
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("after"))
		}
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListOrganizationAlertsPages(t.Context(), "example-org", 5)
	if err != nil {
		t.Fatalf("ListOrganizationAlertsPages() error = %v, want nil", err)
	}
	if got, want := len(result.Alerts), 2; got != want {
		t.Fatalf("len(Alerts) = %d, want %d", got, want)
	}
	if got, want := result.PagesFetched, 2; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false when all pages were fetched within bound")
	}
	if got, want := result.Alerts[0].Repository.FullName, "example-org/alpha-repo"; got != want {
		t.Fatalf("Alerts[0].Repository.FullName = %q, want %q", got, want)
	}
	if got, want := result.Alerts[1].Repository.FullName, "example-org/beta-repo"; got != want {
		t.Fatalf("Alerts[1].Repository.FullName = %q, want %q", got, want)
	}
	if got, want := cursors.values(), []string{"", "org-cursor-2"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cursors = %#v, want %#v", got, want)
	}
}

func TestGitHubDependabotClientMarksOrganizationAlertsTruncatedAtMaxPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+serverURL(t, r)+`?per_page=1&after=more>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{
			Number:     7,
			State:      "open",
			Repository: GitHubDependabotRepository{FullName: "example-org/gamma-repo"},
		}})
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListOrganizationAlertsPages(t.Context(), "example-org", 1)
	if err != nil {
		t.Fatalf("ListOrganizationAlertsPages() error = %v, want nil", err)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true when a next page remains after max_pages")
	}
	if got, want := result.PagesFetched, 1; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
}

func TestGitHubDependabotClientRejectsBlankOrganization(t *testing.T) {
	t.Parallel()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL: "https://api.github.com",
		Token:   "token-value",
	})
	_, err := client.ListOrganizationAlertsPages(t.Context(), "   ", 1)
	if err == nil {
		t.Fatal("ListOrganizationAlertsPages() error = nil, want blank-organization rejection")
	}
}

func TestGitHubDependabotClientRequiresTokenForOrganizationAlerts(t *testing.T) {
	t.Parallel()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL: "https://api.github.com",
	})
	_, err := client.ListOrganizationAlertsPages(t.Context(), "example-org", 1)
	if err == nil {
		t.Fatal("ListOrganizationAlertsPages() error = nil, want token-required error")
	}
}

func TestGitHubDependabotClientDoesNotForwardTokenToCrossHostOrganizationNextLink(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Link", `<http://127.0.0.1:1/orgs/example-org/dependabot/alerts?after=exfiltrate>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]GitHubDependabotAlert{{
			Number:     1,
			State:      "open",
			Repository: GitHubDependabotRepository{FullName: "example-org/alpha-repo"},
		}})
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "token-value",
		RepositoryAlertLimit: 1,
	})
	result, err := client.ListOrganizationAlertsPages(t.Context(), "example-org", 5)
	if err != nil {
		t.Fatalf("ListOrganizationAlertsPages() error = %v, want nil", err)
	}
	if got, want := requests, 1; got != want {
		t.Fatalf("requests = %d, want %d (cross-host next link must be ignored)", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false when cross-host next link is ignored")
	}
}

func TestGitHubDependabotClientReturnsRateLimitFailureForOrganizationAlerts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "token secret-value and org details must not escape", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL: server.URL,
		Token:   "token-value",
	})
	_, err := client.ListOrganizationAlertsPages(t.Context(), "example-org", 2)
	if err == nil {
		t.Fatal("ListOrganizationAlertsPages() error = nil, want rate-limit error")
	}
	var failure GitHubDependabotError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T, want GitHubDependabotError", err)
	}
	if got, want := failure.FailureClass(), GitHubDependabotFailureRateLimited; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if got, want := failure.RateLimit.RetryAfter, 30*time.Second; got != want {
		t.Fatalf("RetryAfter = %v, want %v", got, want)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T, want wrapped sdk.HTTPError", err)
	}
	if strings.Contains(failure.Error(), "secret-value") {
		t.Fatalf("error message leaked upstream body: %q", failure.Error())
	}
}
