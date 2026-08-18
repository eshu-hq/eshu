// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
	"github.com/eshu-hq/eshu/go/internal/cli/hosted"
	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// TestHostedReadinessAdapterMatchesPipelineStatus proves the wrapper feeds the
// deployed pipeline status through the same readiness contract the local scan
// flow uses, and that the resulting verdict lands in the index-readiness
// category an operator sees. The hosted package classifies a verdict; this is
// the half that proves the status shape reaches it unchanged.
func TestHostedReadinessAdapterMatchesPipelineStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   scan.PipelineStatus
		repos    int
		wantCat  hosted.FailCategory
		wantDone bool
	}{
		{
			name:     "ready",
			status:   scan.PipelineStatus{Health: scan.Health{State: "healthy"}, GenerationHistory: scan.GenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hosted.FailNone,
			wantDone: true,
		},
		{
			name:     "empty index",
			status:   scan.PipelineStatus{Health: scan.Health{State: "healthy"}},
			repos:    0,
			wantCat:  hosted.FailEmptyIndex,
			wantDone: false,
		},
		{
			name:     "partial outstanding work",
			status:   scan.PipelineStatus{Health: scan.Health{State: "healthy"}, Queue: scan.Queue{Outstanding: 2, Pending: 2}, GenerationHistory: scan.GenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hosted.FailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "building no generation yet",
			status:   scan.PipelineStatus{Health: scan.Health{State: "healthy"}},
			repos:    1,
			wantCat:  hosted.FailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "stale degraded",
			status:   scan.PipelineStatus{Health: scan.Health{State: "degraded", Reasons: []string{"backend slow"}}, GenerationHistory: scan.GenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hosted.FailStaleReadiness,
			wantDone: false,
		},
		{
			name:     "dead letter work is terminal",
			status:   scan.PipelineStatus{Health: scan.Health{State: "healthy"}, Queue: scan.Queue{DeadLetter: 1}, GenerationHistory: scan.GenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hosted.FailStaleReadiness,
			wantDone: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := hostedStatusServer(t, tc.status)
			client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
			verdict, err := hostedReadinessVerdict(client)
			if err != nil {
				t.Fatalf("hostedReadinessVerdict err = %v, want nil", err)
			}
			cat, _, done := hosted.ClassifyIndexReadiness(verdict, tc.repos)
			if cat != tc.wantCat {
				t.Fatalf("category = %q, want %q", cat, tc.wantCat)
			}
			if done != tc.wantDone {
				t.Fatalf("done = %v, want %v", done, tc.wantDone)
			}
		})
	}
}

// hostedStatusServer serves one canned pipeline status at the hosted status path
// so the adapter is exercised over a real HTTP decode rather than a fake.
func hostedStatusServer(t *testing.T, status scan.PipelineStatus) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != hosted.StatusPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := writeScanJSON(w, status); err != nil {
			t.Errorf("write status: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestHostedRepositoryListProjectsSelectorRules proves the wrapper projects the
// bounded repositories answer into the hosted view and keeps this CLI's
// repository-selector rules attached, so a --repository scope check resolves the
// same way it does for every other command.
func TestHostedRepositoryListProjectsSelectorRules(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v0/repositories") {
			http.NotFound(w, r)
			return
		}
		response := reposelector.ListResponse{
			Repositories: []reposelector.Entry{
				{ID: "repo-1", Name: "acme/api", RepoSlug: "acme-api"},
				{ID: "repo-2", Name: "acme/worker"},
			},
			Total: 2,
		}
		if err := writeScanJSON(w, response); err != nil {
			t.Errorf("write repositories: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	list, err := hostedRepositoryList(client)
	if err != nil {
		t.Fatalf("hostedRepositoryList err = %v, want nil", err)
	}
	if len(list.Repositories) != 2 {
		t.Fatalf("projected %d repositories, want 2", len(list.Repositories))
	}
	if list.Repositories[0].ID != "repo-1" || list.Repositories[0].Name != "acme/api" {
		t.Fatalf("entry[0] = %+v, want repo-1/acme/api", list.Repositories[0])
	}
	// Each entry must carry ITS OWN predicate; a shared closure over the loop
	// variable would make every entry match the last repository.
	if !list.Repositories[0].ScopeMatch("acme/api") {
		t.Fatal("entry[0] does not match its own name")
	}
	if !list.Repositories[0].ScopeMatch("acme-api") {
		t.Fatal("entry[0] does not match its repo slug; selector rules were dropped")
	}
	if list.Repositories[0].ScopeMatch("acme/worker") {
		t.Fatal("entry[0] matched another repository's name")
	}
	if !list.Repositories[1].ScopeMatch("repo-2") {
		t.Fatal("entry[1] does not match its own id")
	}
}

// TestHostedProbeWrapsPathAndEndpoint proves a failing probe names both the path
// and the endpoint, and that a 401 stays inspectable as an auth rejection after
// wrapping.
func TestHostedProbeWrapsPathAndEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	err := hostedProbe(client, hosted.ReadyzPath)
	if err == nil {
		t.Fatal("hostedProbe err = nil, want an auth failure")
	}
	if !strings.Contains(err.Error(), hosted.ReadyzPath) {
		t.Fatalf("probe error %q does not name the probed path", err.Error())
	}
	code, ok := apierr.StatusCode(err)
	if !ok || code != 401 {
		t.Fatalf("wrapped probe error lost its status: (%d, %v)", code, ok)
	}
}

// TestHostedSetupDepsWiresEverySeam proves the production wiring leaves no seam
// nil, since a nil seam would panic at the stage that calls it.
func TestHostedSetupDepsWiresEverySeam(t *testing.T) {
	t.Parallel()
	deps := hostedSetupDeps(&APIClient{BaseURL: "https://eshu.example.com"})
	if deps.Health == nil || deps.Ready == nil || deps.Readiness == nil ||
		deps.ListTools == nil || deps.ListRepos == nil {
		t.Fatalf("hostedSetupDeps left a seam unwired: %+v", deps)
	}
	if len(deps.ListTools()) == 0 {
		t.Fatal("ListTools returned no tools; the MCP read surface is not wired")
	}
}
