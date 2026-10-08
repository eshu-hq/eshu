// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// newGitHubOrgListingServer serves total repositories for org acme with the
// GitHub page/per_page offset arithmetic, every tenth one archived.
func newGitHubOrgListingServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	return newRecordingGitHubOrgListingServer(t, total, nil, nil)
}

// githubListingRequest is one page request the fake GitHub received.
type githubListingRequest struct {
	perPage int
	page    int
}

// newRecordingGitHubOrgListingServer is newGitHubOrgListingServer that also
// appends every page request to requests (when non-nil) and cuts page N to
// shortPages[N] items, as GitHub may when it returns a short page early.
func newRecordingGitHubOrgListingServer(
	t *testing.T,
	total int,
	shortPages map[int]int,
	requests *[]githubListingRequest,
) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			http.Error(w, "bad auth "+got, http.StatusUnauthorized)
			return
		}
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if requests != nil {
			mu.Lock()
			*requests = append(*requests, githubListingRequest{perPage: perPage, page: page})
			mu.Unlock()
		}
		start := (page - 1) * perPage
		end := start + perPage
		if short, ok := shortPages[page]; ok {
			end = start + short
		}
		items := make([]map[string]any, 0, perPage)
		for i := start; i < end && i < total; i++ {
			items = append(items, map[string]any{
				"id":        int64(1000 + i),
				"full_name": fmt.Sprintf("acme/repo-%03d", i),
				"archived":  i%10 == 9,
			})
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestListGitHubOrgRepositoriesReportsCompletenessAndDecodesIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		total        int
		repoLimit    int
		wantCount    int
		wantComplete bool
	}{
		{name: "short last page under the limit", total: 120, repoLimit: 250, wantCount: 120, wantComplete: true},
		{name: "empty page after a full page", total: 100, repoLimit: 200, wantCount: 100, wantComplete: true},
		{name: "full page reaching the limit", total: 150, repoLimit: 100, wantCount: 100, wantComplete: false},
		{name: "exactly the limit with a full last page", total: 100, repoLimit: 100, wantCount: 100, wantComplete: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := newGitHubOrgListingServer(t, tc.total)
			records, complete, err := listGitHubOrgRepositoriesFrom(
				context.Background(), server.Client(), server.URL, "acme", tc.repoLimit, "token-1",
			)
			if err != nil {
				t.Fatalf("listGitHubOrgRepositoriesFrom() error = %v", err)
			}
			if got := len(records); got != tc.wantCount {
				t.Fatalf("record count = %d, want %d", got, tc.wantCount)
			}
			if complete != tc.wantComplete {
				t.Fatalf("complete = %v, want %v", complete, tc.wantComplete)
			}
			if got := records[0]; got.RepoID != "acme/repo-000" || got.GitHubID != 1000 || got.Archived {
				t.Fatalf("records[0] = %+v, want acme/repo-000 with GitHub id 1000, not archived", got)
			}
			if got := records[9]; got.GitHubID != 1009 || !got.Archived {
				t.Fatalf("records[9] = %+v, want GitHub id 1009, archived", got)
			}
		})
	}
}

// TestListGitHubOrgRepositoriesPagesAtAFixedPerPage pins the offset-paging
// contract: page N at per_page P covers items (N-1)*P+1..N*P, so every
// request must use per_page=100 and the limit is applied client-side. A
// smaller per_page on a later page would re-read earlier repositories.
func TestListGitHubOrgRepositoriesPagesAtAFixedPerPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		total        int
		repoLimit    int
		shortPages   map[int]int
		wantCount    int
		wantComplete bool
		wantPages    int
	}{
		{name: "limit above a small org", total: 120, repoLimit: 160, wantCount: 120, wantComplete: true, wantPages: 2},
		{name: "limit above a large org", total: 820, repoLimit: 850, wantCount: 820, wantComplete: true, wantPages: 9},
		{name: "org larger than the limit", total: 250, repoLimit: 100, wantCount: 100, wantComplete: false, wantPages: 1},
		{name: "limit not on a page boundary", total: 250, repoLimit: 150, wantCount: 150, wantComplete: false, wantPages: 2},
		{name: "org exactly the limit stays truncated", total: 150, repoLimit: 150, wantCount: 150, wantComplete: false, wantPages: 2},
		{name: "short page mid-listing ends it", total: 300, repoLimit: 250, shortPages: map[int]int{2: 40}, wantCount: 140, wantComplete: true, wantPages: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requests []githubListingRequest
			server := newRecordingGitHubOrgListingServer(t, tc.total, tc.shortPages, &requests)
			records, complete, err := listGitHubOrgRepositoriesFrom(
				context.Background(), server.Client(), server.URL, "acme", tc.repoLimit, "token-1",
			)
			if err != nil {
				t.Fatalf("listGitHubOrgRepositoriesFrom() error = %v", err)
			}
			if got := len(records); got != tc.wantCount || complete != tc.wantComplete {
				t.Fatalf("listed %d complete %v, want %d complete %v", got, complete, tc.wantCount, tc.wantComplete)
			}
			seen := make(map[string]struct{}, len(records))
			for i, record := range records {
				if want := fmt.Sprintf("acme/repo-%03d", i); record.RepoID != want {
					t.Fatalf("records[%d] = %s, want %s (each repository once, in listing order)", i, record.RepoID, want)
				}
				if _, dup := seen[record.RepoID]; dup {
					t.Fatalf("records list %s twice", record.RepoID)
				}
				seen[record.RepoID] = struct{}{}
			}
			if len(requests) != tc.wantPages {
				t.Fatalf("requests = %+v, want %d pages", requests, tc.wantPages)
			}
			for i, request := range requests {
				if request.perPage != 100 || request.page != i+1 {
					t.Fatalf("request %d = %+v, want per_page=100 page=%d", i, request, i+1)
				}
			}
		})
	}
}

func TestListGitHubOrgRepositoriesFailsOnErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	if _, complete, err := listGitHubOrgRepositoriesFrom(
		context.Background(), server.Client(), server.URL, "acme", 100, "token-1",
	); err == nil || complete {
		t.Fatalf("listGitHubOrgRepositoriesFrom() = complete %v, err %v; want an error and incomplete", complete, err)
	}
}

func TestSelectGitHubRepositoryIDsExposesListingAndRuleExclusions(t *testing.T) {
	t.Parallel()

	got := selectGitHubRepositoryIDs(
		[]GitHubRepositoryRecord{
			{RepoID: "acme/api", GitHubID: 1},
			{RepoID: "acme/web", GitHubID: 2},
			{RepoID: "acme/old", GitHubID: 3, Archived: true},
			{RepoID: "acme/api-legacy", GitHubID: 4, Archived: true},
		},
		[]RepoSyncRepositoryRule{{Kind: "regex", Value: "^acme/api"}},
		false,
	)
	if want := []string{"acme/api"}; fmt.Sprint(got.RepositoryIDs) != fmt.Sprint(want) {
		t.Fatalf("RepositoryIDs = %v, want %v", got.RepositoryIDs, want)
	}
	if want := []string{"acme/web"}; fmt.Sprint(got.RuleExcludedRepositoryIDs) != fmt.Sprint(want) {
		t.Fatalf("RuleExcludedRepositoryIDs = %v, want %v", got.RuleExcludedRepositoryIDs, want)
	}
	if want := []string{"acme/old", "acme/api-legacy"}; fmt.Sprint(got.ArchivedRepositoryIDs) != fmt.Sprint(want) {
		t.Fatalf("ArchivedRepositoryIDs = %v, want %v", got.ArchivedRepositoryIDs, want)
	}
	if got, want := len(got.ListedRepositories), 4; got != want {
		t.Fatalf("len(ListedRepositories) = %d, want %d", got, want)
	}
	if got.ListedRepositories[2].GitHubID != 3 {
		t.Fatalf("ListedRepositories[2] = %+v, want GitHub id 3", got.ListedRepositories[2])
	}

	noRules := selectGitHubRepositoryIDs([]GitHubRepositoryRecord{{RepoID: "acme/web", GitHubID: 2}}, nil, false)
	if len(noRules.RuleExcludedRepositoryIDs) != 0 {
		t.Fatalf("RuleExcludedRepositoryIDs without rules = %v, want empty", noRules.RuleExcludedRepositoryIDs)
	}
}
