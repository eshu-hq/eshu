package ghactionsruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestNumericProviderIDRejectsFractionalFloat(t *testing.T) {
	t.Parallel()

	if _, err := numericProviderID(1001.5); err == nil {
		t.Fatal("numericProviderID(1001.5) error = nil, want integer rejection")
	}
}
