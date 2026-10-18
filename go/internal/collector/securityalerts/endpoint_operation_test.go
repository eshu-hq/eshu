// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAlertListFailuresCarryEndpointTokens proves issue #8258: failed
// Dependabot alert listings must record which endpoint shape failed
// (list_repo_alerts vs list_org_alerts) so failure_operation carries the
// endpoint, not the claim-stage fallback.
func TestAlertListFailuresCarryEndpointTokens(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	t.Cleanup(server.Close)

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              server.URL,
		Token:                "gh-token",
		AllowedRepositories:  []string{"acme/checkout"},
		RepositoryAlertLimit: 10,
		HTTPClient:           server.Client(),
	})

	_, err := client.ListRepositoryAlertsPages(t.Context(), "acme/checkout", 1)
	if err == nil {
		t.Fatal("ListRepositoryAlertsPages() error = nil, want provider failure")
	}
	var repoErr GitHubDependabotError
	if !errors.As(err, &repoErr) {
		t.Fatalf("ListRepositoryAlertsPages() error type = %T, want GitHubDependabotError", err)
	}
	if got, want := repoErr.FailureOperation(), "list_repo_alerts"; got != want {
		t.Fatalf("repo FailureOperation() = %q, want %q", got, want)
	}

	_, err = client.ListOrganizationAlertsPages(t.Context(), "acme", 1)
	if err == nil {
		t.Fatal("ListOrganizationAlertsPages() error = nil, want provider failure")
	}
	var orgErr GitHubDependabotError
	if !errors.As(err, &orgErr) {
		t.Fatalf("ListOrganizationAlertsPages() error type = %T, want GitHubDependabotError", err)
	}
	if got, want := orgErr.FailureOperation(), "list_org_alerts"; got != want {
		t.Fatalf("org FailureOperation() = %q, want %q", got, want)
	}

	for name, dependabotErr := range map[string]GitHubDependabotError{"repo": repoErr, "org": orgErr} {
		if got, want := dependabotErr.FailureClass(), GitHubDependabotFailureRetryable; got != want {
			t.Fatalf("%s FailureClass() = %q, want %q (retry contract must not change)", name, got, want)
		}
		if dependabotErr.TerminalFailure() {
			t.Fatalf("%s TerminalFailure() = true, want false for HTTP 500 (retry contract must not change)", name)
		}
	}
}
