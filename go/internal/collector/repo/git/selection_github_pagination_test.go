// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestListGitHubOrgRepositoriesAlwaysRequestsFullPages is the RED test for
// the #7625 amendment 1 per_page fix: the githubOrg listing must request
// per_page=100 on EVERY page and trim client-side. Before the fix the last
// request shrank per_page to the remaining budget; with offset pagination
// that request re-reads earlier repositories, so the listing could stay
// incomplete while looking complete.
func TestListGitHubOrgRepositoriesAlwaysRequestsFullPages(t *testing.T) {
	// Not parallel: both pagination tests override the shared
	// githubAPIReposBaseURL host.

	const totalRepos = 250
	var perPages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perPage := r.URL.Query().Get("per_page")
		perPages = append(perPages, perPage)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		// Serve GitHub offset-pagination semantics at the REQUESTED width:
		// offset (page-1)*per_page over the full corpus.
		width, _ := strconv.Atoi(perPage)
		start := (page - 1) * width
		payload := make([]map[string]any, 0)
		for i := start; i < start+width && i < totalRepos; i++ {
			payload = append(payload, map[string]any{
				"id":        1000 + i,
				"full_name": "boatsgroup/repo-" + strconv.Itoa(i),
				"archived":  false,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode page: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	previousBase := githubAPIReposBaseURL
	githubAPIReposBaseURL = server.URL
	t.Cleanup(func() { githubAPIReposBaseURL = previousBase })

	repositories, truncated, err := listGitHubOrgRepositories(context.Background(), "boatsgroup", 150, "token")
	if err != nil {
		t.Fatalf("listGitHubOrgRepositories() error = %v", err)
	}
	for i, perPage := range perPages {
		if perPage != "100" {
			t.Fatalf("request %d per_page = %q, want constant %q (shrinking per_page re-reads earlier repos under offset pagination)", i+1, perPage, "100")
		}
	}
	if len(repositories) != 150 {
		t.Fatalf("len(repositories) = %d, want 150 (client-side trim of full pages)", len(repositories))
	}
	if !truncated {
		t.Fatalf("truncated = false, want true (250 repos at limit 150 stopped before exhaustion)")
	}
	seen := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		if _, dup := seen[repository.RepoID]; dup {
			t.Fatalf("duplicate repository %q: listing re-read an earlier page", repository.RepoID)
		}
		seen[repository.RepoID] = struct{}{}
		if repository.GitHubID == 0 {
			t.Fatalf("repository %q GitHubID = 0, want the decoded numeric id", repository.RepoID)
		}
	}
}

// TestListGitHubOrgRepositoriesReportsExhaustedListing proves a listing that
// reaches an empty page reports truncated=false, so the selection observer
// evaluates from it.
func TestListGitHubOrgRepositoriesReportsExhaustedListing(t *testing.T) {
	// Not parallel: both pagination tests override the shared
	// githubAPIReposBaseURL host.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		payload := make([]map[string]any, 0)
		if page == 1 {
			payload = append(payload, map[string]any{
				"id":        4242,
				"full_name": "boatsgroup/only-repo",
				"archived":  false,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode page: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	previousBase := githubAPIReposBaseURL
	githubAPIReposBaseURL = server.URL
	t.Cleanup(func() { githubAPIReposBaseURL = previousBase })

	repositories, truncated, err := listGitHubOrgRepositories(context.Background(), "boatsgroup", 4000, "token")
	if err != nil {
		t.Fatalf("listGitHubOrgRepositories() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false (empty page ended the listing)")
	}
	if len(repositories) != 1 || repositories[0].RepoID != "boatsgroup/only-repo" {
		t.Fatalf("repositories = %+v, want the single listed repo", repositories)
	}
	if repositories[0].GitHubID != 4242 {
		t.Fatalf("GitHubID = %d, want 4242", repositories[0].GitHubID)
	}
}
