// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	alertsDisabledBody  = `{"message":"Dependabot alerts are disabled for this repository.","documentation_url":"https://docs.github.com/rest/dependabot/alerts","status":"403"}`
	alertsDisabledToken = "must-not-escape-secret"
)

// TestGitHubDependabotClientMarksAlertsDisabled403 pins when a failed alert
// read carries the AlertsDisabled marker: only a 403 with no rate-limit signal
// whose JSON message says Dependabot alerts are disabled. Every other shape
// keeps today's classification with the marker false.
func TestGitHubDependabotClientMarksAlertsDisabled403(t *testing.T) {
	t.Parallel()

	cut := `{"message":"Dependabot alerts are disabled for this repository.","pad":"` +
		strings.Repeat("x", maxGitHubErrorBodyBytes) + `"}`
	// phrase is complete JSON; padding it with trailing spaces pins the body
	// cap edge: exactly maxGitHubErrorBodyBytes is read in full, one byte more
	// is cut off and never interpreted.
	const phrase = `{"message":"Dependabot alerts are disabled for this repository."}`
	atCap := phrase + strings.Repeat(" ", maxGitHubErrorBodyBytes-len(phrase))
	overCap := atCap + " "
	tests := []struct {
		name      string
		status    int
		headers   map[string]string
		body      string
		wantMark  bool
		wantClass string
	}{
		{name: "disabled 403", status: 403, body: alertsDisabledBody, wantMark: true, wantClass: GitHubDependabotFailureNotFound},
		{name: "lowercase wording", status: 403, body: `{"message":"dependabot alerts are disabled for this repository."}`, wantMark: true, wantClass: GitHubDependabotFailureNotFound},
		{name: "rate limit remaining zero wins", status: 403, headers: map[string]string{"X-RateLimit-Remaining": "0"}, body: alertsDisabledBody, wantClass: GitHubDependabotFailureRateLimited},
		{name: "retry-after wins", status: 403, headers: map[string]string{"Retry-After": "30"}, body: alertsDisabledBody, wantClass: GitHubDependabotFailureRateLimited},
		{name: "integration permission 403", status: 403, body: `{"message":"Resource not accessible by integration"}`, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "html body", status: 403, body: `<html>Dependabot alerts are disabled</html>`, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "empty body", status: 403, body: ``, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "no message", status: 403, body: `{"documentation_url":"x"}`, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "non-string message", status: 403, body: `{"message":["Dependabot alerts are disabled"]}`, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "cut off past the cap", status: 403, body: cut, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "different wording", status: 403, body: `{"message":"Alerts are turned off"}`, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "complete body of exactly the cap", status: 403, body: atCap, wantMark: true, wantClass: GitHubDependabotFailureNotFound},
		{name: "body one byte over the cap", status: 403, body: overCap, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "401 with the text", status: 401, body: alertsDisabledBody, wantClass: GitHubDependabotFailureAuthDenied},
		{name: "404 with the text", status: 404, body: alertsDisabledBody, wantClass: GitHubDependabotFailureNotFound},
		{name: "422 with the text", status: 422, body: alertsDisabledBody, wantClass: GitHubDependabotFailureTerminal},
		{name: "500 with the text", status: 500, body: alertsDisabledBody, wantClass: GitHubDependabotFailureRetryable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
				BaseURL:             server.URL,
				Token:               alertsDisabledToken,
				AllowedRepositories: []string{"example-org/example-repo"},
			})
			_, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 1)
			var failure GitHubDependabotError
			if !errors.As(err, &failure) {
				t.Fatalf("error = %T (%v), want GitHubDependabotError", err, err)
			}
			if failure.AlertsDisabled != tt.wantMark {
				t.Fatalf("AlertsDisabled = %t, want %t", failure.AlertsDisabled, tt.wantMark)
			}
			if got := failure.FailureClass(); got != tt.wantClass {
				t.Fatalf("FailureClass = %q, want %q", got, tt.wantClass)
			}
			if text := strings.ToLower(failure.Error()); strings.Contains(text, "disabled") || strings.Contains(text, alertsDisabledToken) {
				t.Fatalf("Error() = %q, want no response body text", failure.Error())
			}
		})
	}
}

// TestGitHubDependabotClientAlertsDisabled403IsTerminalNotFound proves a
// repository-scope read that gets the disabled 403 fails as terminal
// not_found, the documented class for a disabled alert surface (#8947), so
// readiness does not tell the operator to rotate a credential that works.
func TestGitHubDependabotClientAlertsDisabled403IsTerminalNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(alertsDisabledBody))
	}))
	defer server.Close()
	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             server.URL,
		Token:               "token-value",
		AllowedRepositories: []string{"example-org/example-repo"},
	})
	_, err := client.ListRepositoryAlertsPages(t.Context(), "example-org/example-repo", 1)
	var failure GitHubDependabotError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T, want GitHubDependabotError", err)
	}
	if !failure.TerminalFailure() || failure.FailureClass() != GitHubDependabotFailureNotFound {
		t.Fatalf("class = %q terminal = %t, want terminal not_found", failure.FailureClass(), failure.TerminalFailure())
	}
}
