// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file holds the issue #5389 artifact-list truncation test, split out of
// client_test.go to keep that file under the repo's 500-line cap.

// TestGitHubClientFetchRunsMarksArtifactsPartialWhenTotalCountExceedsFetchedArtifacts
// is the RED->GREEN proof for issue #5389: fetchArtifacts used to decode only
// {Artifacts []map[string]any} with no total_count field, so a truncated
// artifact page (more artifacts than the fetched MaxArtifacts window) was
// silent — unlike the runs (Truncated) and jobs (JobsPartial) windows, which
// both already surface their own truncation signal. The provider's
// total_count exceeding the returned artifacts length must now set
// snapshot.ArtifactsPartial, mirroring fetchJobs' JobsPartial derivation.
func TestGitHubClientFetchRunsMarksArtifactsPartialWhenTotalCountExceedsFetchedArtifacts(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo/actions/runs":
			_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[
				{"id":1001,"head_sha":"0123456789abcdef0123456789abcdef01234567","repository":{"full_name":"example/repo"}}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			_, _ = w.Write([]byte(`{"total_count":0,"jobs":[]}`))
		case strings.HasSuffix(r.URL.Path, "/artifacts"):
			if got, want := r.URL.Query().Get("per_page"), "1"; got != want {
				t.Fatalf("per_page = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"total_count":3,"artifacts":[{"id":3001,"name":"image-digest"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	page, err := (GitHubClient{HTTPClient: server.Client()}).FetchRuns(context.Background(), TargetConfig{
		ScopeID:             "ci-cd:github-actions:example/repo",
		Repository:          "example/repo",
		Token:               "token",
		AllowedRepositories: []string{"example/repo"},
		APIBaseURL:          server.URL,
		MaxRuns:             1,
		MaxJobs:             10,
		MaxArtifacts:        1,
	})
	if err != nil {
		t.Fatalf("FetchRuns() error = %v, want nil", err)
	}
	if got, want := len(page.Snapshots), 1; got != want {
		t.Fatalf("len(Snapshots) = %d, want %d", got, want)
	}
	snapshot := page.Snapshots[0]
	if got, want := len(snapshot.Artifacts), 1; got != want {
		t.Fatalf("len(Artifacts) = %d, want %d", got, want)
	}
	if !snapshot.ArtifactsPartial {
		t.Fatal("ArtifactsPartial = false, want true from total_count > returned artifacts")
	}
}
