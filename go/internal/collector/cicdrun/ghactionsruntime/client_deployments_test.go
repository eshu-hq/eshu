// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func deploymentsTargetConfig(server *httptest.Server, maxDeployments int) TargetConfig {
	return TargetConfig{
		ScopeID:             "ci-cd:github-actions:example/repo",
		Repository:          "example/repo",
		Token:               "token-value",
		AllowedRepositories: []string{"example/repo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             1,
		MaxArtifacts:        1,
		MaxDeployments:      maxDeployments,
	}
}

// TestGitHubClientFetchDeploymentsSendsPerPageBoundAndParsesArray covers test
// 1: fetchDeployments sends the per_page bound derived from MaxDeployments
// and parses the bare JSON array GitHub's deployments-list endpoint returns
// (unlike actions/runs, jobs, and artifacts, this endpoint has no
// total_count wrapper).
func TestGitHubClientFetchDeploymentsSendsPerPageBoundAndParsesArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo/deployments":
			if got, want := r.URL.Query().Get("per_page"), "5"; got != want {
				t.Fatalf("per_page = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`[{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"}]`))
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitHubClient{HTTPClient: server.Client()}).FetchDeployments(context.Background(), deploymentsTargetConfig(server, 5))
	if err != nil {
		t.Fatalf("FetchDeployments() error = %v, want nil", err)
	}
	if got, want := len(page.Snapshots), 1; got != want {
		t.Fatalf("len(Snapshots) = %d, want %d", got, want)
	}
	if got, want := page.Snapshots[0].Deployment["environment"], "production"; got != want {
		t.Fatalf("Deployment[environment] = %#v, want %#v", got, want)
	}
}

// TestGitHubClientFetchDeploymentsFetchesStatusesPerDeploymentAndParsesState
// covers test 2: the statuses fetch is issued per deployment (keyed by the
// deployment's own id in the URL path) and parses the "state" field of each
// returned status.
func TestGitHubClientFetchDeploymentsFetchesStatusesPerDeploymentAndParsesState(t *testing.T) {
	t.Parallel()

	var statusPaths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo/deployments":
			_, _ = w.Write([]byte(`[
				{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"},
				{"id":9002,"sha":"1123456789abcdef0123456789abcdef01234567","environment":"staging"}
			]`))
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			statusPaths = append(statusPaths, r.URL.Path)
			if strings.Contains(r.URL.Path, "9001") {
				_, _ = w.Write([]byte(`[{"id":8001,"state":"success"}]`))
			} else {
				_, _ = w.Write([]byte(`[{"id":8002,"state":"pending"}]`))
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitHubClient{HTTPClient: server.Client()}).FetchDeployments(context.Background(), deploymentsTargetConfig(server, 10))
	if err != nil {
		t.Fatalf("FetchDeployments() error = %v, want nil", err)
	}
	wantStatusPaths := []string{
		"/repos/example/repo/deployments/9001/statuses",
		"/repos/example/repo/deployments/9002/statuses",
	}
	if got, want := len(statusPaths), len(wantStatusPaths); got != want {
		t.Fatalf("len(statusPaths) = %d, want %d: %#v", got, want, statusPaths)
	}
	for i, want := range wantStatusPaths {
		if statusPaths[i] != want {
			t.Fatalf("statusPaths[%d] = %q, want %q", i, statusPaths[i], want)
		}
	}
	if got, want := len(page.Snapshots), 2; got != want {
		t.Fatalf("len(Snapshots) = %d, want %d", got, want)
	}
	if got, want := page.Snapshots[0].Statuses[0]["state"], "success"; got != want {
		t.Fatalf("Snapshots[0].Statuses[0][state] = %#v, want %#v", got, want)
	}
	if got, want := page.Snapshots[1].Statuses[0]["state"], "pending"; got != want {
		t.Fatalf("Snapshots[1].Statuses[0][state] = %#v, want %#v", got, want)
	}
}

// TestGitHubClientFetchDeploymentsReportsTruncationWhenCountHitsBound covers
// test 3: GitHub's deployments-list endpoint carries no total_count, so
// truncation is the same full-page heuristic runsPageTruncated falls back to
// -- the fetched length equaling the requested MaxDeployments bound signals
// more deployments may exist beyond the window.
func TestGitHubClientFetchDeploymentsReportsTruncationWhenCountHitsBound(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo/deployments":
			_, _ = w.Write([]byte(`[
				{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"},
				{"id":9002,"sha":"1123456789abcdef0123456789abcdef01234567","environment":"production"}
			]`))
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitHubClient{HTTPClient: server.Client()}).FetchDeployments(context.Background(), deploymentsTargetConfig(server, 2))
	if err != nil {
		t.Fatalf("FetchDeployments() error = %v, want nil", err)
	}
	if !page.Truncated {
		t.Fatal("Truncated = false, want true (fetched length 2 == MaxDeployments 2)")
	}
}

// TestGitHubClientFetchDeploymentsMapsRateLimitThroughExistingClassification
// covers test 4: a rate-limited response from the deployments endpoint maps
// through the SAME rate-limit classification (rateLimitErrorFromResponse via
// getJSON) FetchRuns already uses, rather than a second error path.
func TestGitHubClientFetchDeploymentsMapsRateLimitThroughExistingClassification(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "provider rate limit", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, err := (GitHubClient{HTTPClient: server.Client()}).FetchDeployments(context.Background(), deploymentsTargetConfig(server, 5))
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchDeployments() error = %v, want ErrRateLimited", err)
	}
	var rateLimited RateLimitError
	if !errors.As(err, &rateLimited) {
		t.Fatalf("FetchDeployments() error = %T, want RateLimitError", err)
	}
}

// TestGitHubClientFetchDeploymentsMapsNon200WithoutLeakingToken covers test
// 5: a non-200 response maps to a bounded sdk.HTTPError that never leaks the
// provider response body or the request token.
func TestGitHubClientFetchDeploymentsMapsNon200WithoutLeakingToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider body mentions token-value and example/repo", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, err := (GitHubClient{HTTPClient: server.Client()}).FetchDeployments(context.Background(), deploymentsTargetConfig(server, 5))
	if err == nil {
		t.Fatal("FetchDeployments() error = nil, want permission failure")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchDeployments() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusForbidden; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-value") || strings.Contains(err.Error(), "example/repo") {
		t.Fatalf("FetchDeployments() error leaked provider body: %q", err)
	}
}
