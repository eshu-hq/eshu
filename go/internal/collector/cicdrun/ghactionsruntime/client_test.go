// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestGitHubClientReturnsBoundedSDKHTTPErrorForPermissionFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider body mentions token-value and example/repo", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, err := (GitHubClient{HTTPClient: server.Client()}).FetchLatestRun(t.Context(), TargetConfig{
		ScopeID:             "ci-cd:github-actions:example/repo",
		Repository:          "example/repo",
		Token:               "token-value",
		AllowedRepositories: []string{"example/repo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             1,
		MaxArtifacts:        1,
	})
	if err == nil {
		t.Fatal("FetchLatestRun() error = nil, want permission failure")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchLatestRun() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusForbidden; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-value") || strings.Contains(err.Error(), "example/repo") {
		t.Fatalf("FetchLatestRun() error leaked provider body: %q", err)
	}
}

func TestGitHubClientFetchLatestRunUsesBoundedActionsEndpoints(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		if got, want := r.Header.Get("Authorization"), "Bearer token"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/repos/example/repo/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1001,"workflow_id":42,"name":"Publish","run_attempt":1,"head_sha":"0123456789abcdef0123456789abcdef01234567","repository":{"full_name":"example/repo"}}]}`))
		case "/repos/example/repo/actions/runs/1001/jobs":
			_, _ = w.Write([]byte(`{"total_count":2,"jobs":[{"id":2001,"name":"build"}]}`))
		case "/repos/example/repo/actions/runs/1001/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":3001,"name":"image-digest","workflow_run":{"id":1001,"head_sha":"0123456789abcdef0123456789abcdef01234567"}}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	snapshot, err := (GitHubClient{HTTPClient: server.Client()}).FetchLatestRun(context.Background(), TargetConfig{
		ScopeID:             "ci-cd:github-actions:example/repo",
		Repository:          "example/repo",
		Token:               "token",
		AllowedRepositories: []string{"example/repo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             1,
		MaxArtifacts:        1,
	})
	if err != nil {
		t.Fatalf("FetchLatestRun() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Jobs), 1; got != want {
		t.Fatalf("len(Jobs) = %d, want %d", got, want)
	}
	if !snapshot.JobsPartial {
		t.Fatal("JobsPartial = false, want true from total_count > returned jobs")
	}
	if got, want := len(snapshot.Artifacts), 1; got != want {
		t.Fatalf("len(Artifacts) = %d, want %d", got, want)
	}
	wantPaths := []string{
		"/repos/example/repo/actions/runs?per_page=1",
		"/repos/example/repo/actions/runs/1001/jobs?per_page=1",
		"/repos/example/repo/actions/runs/1001/artifacts?per_page=1",
	}
	if got, want := len(paths), len(wantPaths); got != want {
		t.Fatalf("len(paths) = %d, want %d: %#v", got, want, paths)
	}
	for i, want := range wantPaths {
		if paths[i] != want {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], want)
		}
	}
}

func TestGitHubClientDistinguishesRateLimitsFromPermissionFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status              int
		rateLimitRemaining  string
		wantRateLimited     bool
		wantPermissionError bool
	}{
		"forbidden permission failure": {
			status:              http.StatusForbidden,
			wantPermissionError: true,
		},
		"forbidden exhausted rate limit": {
			status:             http.StatusForbidden,
			rateLimitRemaining: "0",
			wantRateLimited:    true,
		},
		"too many requests": {
			status:          http.StatusTooManyRequests,
			wantRateLimited: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.rateLimitRemaining != "" {
					w.Header().Set("X-RateLimit-Remaining", tc.rateLimitRemaining)
				}
				http.Error(w, "provider denied request", tc.status)
			}))
			t.Cleanup(server.Close)

			_, err := (GitHubClient{HTTPClient: server.Client()}).FetchLatestRun(t.Context(), TargetConfig{
				ScopeID:             "ci-cd:github-actions:example/repo",
				Repository:          "example/repo",
				Token:               "token",
				AllowedRepositories: []string{"example/repo"},
				APIBaseURL:          server.URL,
				MaxRuns:             1,
				MaxJobs:             1,
				MaxArtifacts:        1,
			})
			switch {
			case tc.wantRateLimited && !errors.Is(err, ErrRateLimited):
				t.Fatalf("FetchLatestRun() error = %v, want ErrRateLimited", err)
			case tc.wantPermissionError && (err == nil || errors.Is(err, ErrRateLimited)):
				t.Fatalf("FetchLatestRun() error = %v, want non-rate-limit permission error", err)
			}
		})
	}
}

func TestGitHubClientReturnsRateLimitRetryGuidance(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status    int
		headers   map[string]string
		wantDelay time.Duration
		wantReset bool
	}{
		"retry after": {
			status: http.StatusTooManyRequests,
			headers: map[string]string{
				"Retry-After": "45",
			},
			wantDelay: 45 * time.Second,
		},
		"primary reset": {
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
			},
			wantReset: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				http.Error(w, "provider rate limit", tc.status)
			}))
			t.Cleanup(server.Close)

			_, err := (GitHubClient{HTTPClient: server.Client()}).FetchLatestRun(t.Context(), TargetConfig{
				ScopeID:             "ci-cd:github-actions:example/repo",
				Repository:          "example/repo",
				Token:               "token",
				AllowedRepositories: []string{"example/repo"},
				APIBaseURL:          server.URL,
				MaxRuns:             1,
				MaxJobs:             1,
				MaxArtifacts:        1,
			})
			if !errors.Is(err, ErrRateLimited) {
				t.Fatalf("FetchLatestRun() error = %v, want ErrRateLimited", err)
			}
			var rateLimited RateLimitError
			if !errors.As(err, &rateLimited) {
				t.Fatalf("FetchLatestRun() error = %T, want RateLimitError", err)
			}
			if tc.wantDelay > 0 && rateLimited.RetryAfter != tc.wantDelay {
				t.Fatalf("RetryAfter = %v, want %v", rateLimited.RetryAfter, tc.wantDelay)
			}
			if tc.wantReset && (rateLimited.RetryAfter < 50*time.Minute || rateLimited.Reset.IsZero()) {
				t.Fatalf("rate limit guidance = %#v, want reset-derived delay", rateLimited)
			}
		})
	}
}

func TestNumericProviderIDRejectsFractionalFloat(t *testing.T) {
	t.Parallel()

	if _, err := numericProviderID(1001.5); err == nil {
		t.Fatal("numericProviderID(1001.5) error = nil, want integer rejection")
	}
}
