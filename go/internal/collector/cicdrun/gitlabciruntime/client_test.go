// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestGitLabClientReturnsBoundedSDKHTTPErrorForPermissionFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider body mentions token-value and eshu-hq/demo", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, err := (GitLabClient{HTTPClient: server.Client()}).FetchPipelines(t.Context(), TargetConfig{
		ScopeID:             "gitlab-ci://gitlab.example.com/eshu-hq/demo",
		ProjectPath:         "eshu-hq/demo",
		Token:               "token-value",
		AllowedProjectPaths: []string{"eshu-hq/demo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             1,
	})
	if err == nil {
		t.Fatal("FetchPipelines() error = nil, want permission failure")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchPipelines() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusForbidden; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-value") || strings.Contains(err.Error(), "eshu-hq/demo") {
		t.Fatalf("FetchPipelines() error leaked provider body: %q", err)
	}
}

func TestGitLabClientFetchPipelinesUsesBoundedPipelinesAndJobsEndpoints(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		if got, want := r.Header.Get("PRIVATE-TOKEN"), "token"; got != want {
			t.Fatalf("PRIVATE-TOKEN = %q, want %q", got, want)
		}
		switch r.URL.EscapedPath() {
		case "/projects/eshu-hq%2Fdemo/pipelines":
			w.Header().Set("X-Total", "1")
			_, _ = w.Write([]byte(`[{"id":7700,"iid":42,"project_id":1,"status":"success","source":"push","ref":"main","sha":"1f2e3d4c5b6a79889706a5b4c3d2e1f00f1e2d3c","web_url":"https://gitlab.example.com/eshu-hq/demo/-/pipelines/7700","created_at":"2026-06-07T14:58:00Z","updated_at":"2026-06-07T15:00:00Z"}]`))
		case "/projects/eshu-hq%2Fdemo/pipelines/7700":
			_, _ = w.Write([]byte(`{"id":7700,"iid":42,"project_id":1,"status":"success","source":"push","ref":"main","sha":"1f2e3d4c5b6a79889706a5b4c3d2e1f00f1e2d3c","web_url":"https://gitlab.example.com/eshu-hq/demo/-/pipelines/7700","created_at":"2026-06-07T14:58:00Z","updated_at":"2026-06-07T15:00:00Z","started_at":"2026-06-07T14:59:00Z","finished_at":"2026-06-07T15:00:00Z","user":{"username":"builder"}}`))
		case "/projects/eshu-hq%2Fdemo/pipelines/7700/jobs":
			w.Header().Set("X-Total", "1")
			_, _ = w.Write([]byte(`[{"id":9001,"name":"build","stage":"build","status":"success","created_at":"2026-06-07T14:58:10Z","started_at":"2026-06-07T14:58:20Z","finished_at":"2026-06-07T14:59:50Z","web_url":"https://gitlab.example.com/eshu-hq/demo/-/jobs/9001","artifacts":[{"file_type":"archive","size":128,"filename":"artifacts.zip","file_format":"zip"}]}]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitLabClient{HTTPClient: server.Client()}).FetchPipelines(context.Background(), TargetConfig{
		ScopeID:             "gitlab-ci://gitlab.example.com/eshu-hq/demo",
		ProjectPath:         "eshu-hq/demo",
		Token:               "token",
		AllowedProjectPaths: []string{"eshu-hq/demo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             10,
	})
	if err != nil {
		t.Fatalf("FetchPipelines() error = %v, want nil", err)
	}
	if got, want := len(page.Snapshots), 1; got != want {
		t.Fatalf("len(Snapshots) = %d, want %d", got, want)
	}
	if page.Truncated {
		t.Fatal("Truncated = true, want false (X-Total matched fetched length)")
	}
	snapshot := page.Snapshots[0]
	if got, want := snapshot.Pipeline["started_at"], "2026-06-07T14:59:00Z"; got != want {
		t.Fatalf("Pipeline[started_at] = %#v, want %#v (must come from the detail GET, not the list)", got, want)
	}
	if got, want := len(snapshot.Jobs), 1; got != want {
		t.Fatalf("len(Jobs) = %d, want %d", got, want)
	}
	if snapshot.JobsPartial {
		t.Fatal("JobsPartial = true, want false")
	}
	wantPaths := []string{
		"/projects/eshu-hq%2Fdemo/pipelines",
		"/projects/eshu-hq%2Fdemo/pipelines/7700",
		"/projects/eshu-hq%2Fdemo/pipelines/7700/jobs",
	}
	if got := paths; len(got) != len(wantPaths) {
		t.Fatalf("requested paths = %v, want %v", got, wantPaths)
	} else {
		for i, want := range wantPaths {
			if got[i] != want {
				t.Fatalf("requested paths[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
}

func TestGitLabClientReportsTruncatedPipelinesWindow(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.EscapedPath() == "/projects/eshu-hq%2Fdemo/pipelines":
			w.Header().Set("X-Total", "5")
			_, _ = w.Write([]byte(`[{"id":2,"status":"success","source":"push","ref":"main","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","web_url":"https://gitlab.example.com/eshu-hq/demo/-/pipelines/2"}]`))
		case strings.HasSuffix(r.URL.EscapedPath(), "/jobs"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"id":2,"status":"success","source":"push","ref":"main","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","web_url":"https://gitlab.example.com/eshu-hq/demo/-/pipelines/2"}`))
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitLabClient{HTTPClient: server.Client()}).FetchPipelines(context.Background(), TargetConfig{
		ScopeID:             "gitlab-ci://gitlab.example.com/eshu-hq/demo",
		ProjectPath:         "eshu-hq/demo",
		Token:               "token",
		AllowedProjectPaths: []string{"eshu-hq/demo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             10,
	})
	if err != nil {
		t.Fatalf("FetchPipelines() error = %v, want nil", err)
	}
	if !page.Truncated {
		t.Fatal("Truncated = false, want true (X-Total=5 exceeds the fetched window of 1)")
	}
}

func TestGitLabClientClassifiesRateLimitResponse(t *testing.T) {
	t.Parallel()

	resetTime := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("RateLimit-ResetTime", resetTime)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	_, err := (GitLabClient{HTTPClient: server.Client()}).FetchPipelines(context.Background(), TargetConfig{
		ScopeID:             "gitlab-ci://gitlab.example.com/eshu-hq/demo",
		ProjectPath:         "eshu-hq/demo",
		Token:               "token",
		AllowedProjectPaths: []string{"eshu-hq/demo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             10,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchPipelines() error = %v, want ErrRateLimited", err)
	}
	var rateLimitErr RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("FetchPipelines() error = %T, want RateLimitError", err)
	}
	if rateLimitErr.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v, want positive", rateLimitErr.RetryAfter)
	}
}

func TestGitLabClientRejectsNoPipelines(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	_, err := (GitLabClient{HTTPClient: server.Client()}).FetchPipelines(context.Background(), TargetConfig{
		ScopeID:             "gitlab-ci://gitlab.example.com/eshu-hq/demo",
		ProjectPath:         "eshu-hq/demo",
		Token:               "token",
		AllowedProjectPaths: []string{"eshu-hq/demo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             10,
	})
	if err == nil {
		t.Fatal("FetchPipelines() error = nil, want no-pipelines error")
	}
}
