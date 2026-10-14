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
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
)

// newGitHubOrgListingServer serves total repositories for org acme with the
// GitHub page/per_page offset arithmetic, every tenth one archived.
func newGitHubOrgListingServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	return newRecordingGitHubOrgListingServer(t, total, nil, nil, nil)
}

// githubListingRequest is one page request the fake GitHub received.
type githubListingRequest struct {
	perPage int
	page    int
}

// newRecordingGitHubOrgListingServer is newGitHubOrgListingServer that also
// appends every page request to requests (when non-nil), cuts page N to
// shortPages[N] items, as GitHub may when it returns a short page early, and
// sends a Link rel="next" header on every page in linkNextPages.
func newRecordingGitHubOrgListingServer(
	t *testing.T,
	total int,
	shortPages map[int]int,
	linkNextPages map[int]bool,
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
		if linkNextPages[page] {
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/acme/repos?per_page=%d&page=%d>; rel="next"`, "http://"+r.Host, perPage, page+1))
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
// smaller per_page on a later page would re-read earlier repositories. Only
// an empty page or the limit ends the listing: a short page mid-listing must
// not stop ingestion of the repositories after it. A listing is complete only
// when its empty page carries no Link rel="next".
func TestListGitHubOrgRepositoriesPagesAtAFixedPerPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		total         int
		repoLimit     int
		shortPages    map[int]int
		linkNextPages map[int]bool
		wantComplete  bool
		wantPages     int
		wantIDRanges  [][2]int // half-open [from, to) ranges of repo-NNN, in order
	}{
		{name: "limit above a small org", total: 120, repoLimit: 160, wantComplete: true, wantPages: 3, wantIDRanges: [][2]int{{0, 120}}},
		{name: "limit above a large org", total: 820, repoLimit: 850, wantComplete: true, wantPages: 10, wantIDRanges: [][2]int{{0, 820}}},
		{name: "org larger than the limit", total: 250, repoLimit: 100, wantComplete: false, wantPages: 1, wantIDRanges: [][2]int{{0, 100}}},
		{name: "limit not on a page boundary", total: 250, repoLimit: 150, wantComplete: false, wantPages: 2, wantIDRanges: [][2]int{{0, 150}}},
		{name: "org exactly the limit stays truncated", total: 150, repoLimit: 150, wantComplete: false, wantPages: 2, wantIDRanges: [][2]int{{0, 150}}},
		{name: "short page mid-listing continues", total: 300, repoLimit: 250, shortPages: map[int]int{2: 40}, wantComplete: true, wantPages: 4, wantIDRanges: [][2]int{{0, 140}, {200, 300}}},
		{name: "short page mid-listing then the limit", total: 300, repoLimit: 200, shortPages: map[int]int{2: 40}, wantComplete: false, wantPages: 3, wantIDRanges: [][2]int{{0, 140}, {200, 260}}},
		{name: "empty page that still links a next page", total: 300, repoLimit: 400, shortPages: map[int]int{2: 0}, linkNextPages: map[int]bool{2: true}, wantComplete: false, wantPages: 2, wantIDRanges: [][2]int{{0, 100}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requests []githubListingRequest
			server := newRecordingGitHubOrgListingServer(t, tc.total, tc.shortPages, tc.linkNextPages, &requests)
			records, complete, err := listGitHubOrgRepositoriesFrom(
				context.Background(), server.Client(), server.URL, "acme", tc.repoLimit, "token-1",
			)
			if err != nil {
				t.Fatalf("listGitHubOrgRepositoriesFrom() error = %v", err)
			}
			var wantIDs []string
			for _, r := range tc.wantIDRanges {
				for i := r[0]; i < r[1]; i++ {
					wantIDs = append(wantIDs, fmt.Sprintf("acme/repo-%03d", i))
				}
			}
			if got := len(records); got != len(wantIDs) || complete != tc.wantComplete {
				t.Fatalf("listed %d complete %v, want %d complete %v", got, complete, len(wantIDs), tc.wantComplete)
			}
			seen := make(map[string]struct{}, len(records))
			for i, record := range records {
				if record.RepoID != wantIDs[i] {
					t.Fatalf("records[%d] = %s, want %s (each repository once, in listing order)", i, record.RepoID, wantIDs[i])
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

func TestLinkHeaderHasNext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		values []string
		want   bool
	}{
		{name: "no header", want: false},
		{name: "prev and last only", values: []string{`<https://x/?page=1>; rel="prev", <https://x/?page=3>; rel="last"`}, want: false},
		{name: "next among others", values: []string{`<https://x/?page=1>; rel="prev", <https://x/?page=3>; rel="next"`}, want: true},
		{name: "space-separated rel types", values: []string{`<https://x/?page=3>; rel="last next"`}, want: true},
		{name: "unquoted and upper case", values: []string{`<https://x/?page=3>; REL=Next`}, want: true},
		{name: "second header value", values: []string{`<https://x/?page=1>; rel="prev"`, `<https://x/?page=3>; rel="next"`}, want: true},
		{name: "next only in the target", values: []string{`<https://x/?rel=next>; rel="last"`}, want: false},
	}
	for _, tc := range cases {
		if got := linkHeaderHasNext(tc.values); got != tc.want {
			t.Errorf("%s: linkHeaderHasNext(%q) = %v, want %v", tc.name, tc.values, got, tc.want)
		}
	}
}

func TestListGitHubOrgRepositoriesFailsOnErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	// No-op Sleep: the production path waits out real backoffs, which the
	// retry-schedule test above pins without waiting.
	noWait := rest.Config{MaxAttempts: 4, BaseBackoff: time.Second, Sleep: func(context.Context, time.Duration) error { return nil }}
	if _, complete, err := listGitHubOrgRepositoriesFromWithRetry(
		context.Background(), server.Client(), server.URL, "acme", 100, "token-1", noWait,
	); err == nil || complete {
		t.Fatalf("listGitHubOrgRepositoriesFromWithRetry() = complete %v, err %v; want an error and incomplete", complete, err)
	}
}

// TestListGitHubOrgRepositoriesRetriesTransientServerErrors is the #8076
// regression: a transient 502 on the org listing must not end the listing.
// The first two page-1 requests fail with 502, then the server recovers; the
// listing must succeed, report complete, and show three page-1 attempts with
// the documented backoff waits and no real waiting.
func TestListGitHubOrgRepositoriesRetriesTransientServerErrors(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var pageOneAttempts int
	var waits []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			mu.Lock()
			pageOneAttempts++
			fail := pageOneAttempts <= 2
			mu.Unlock()
			if fail {
				http.Error(w, `{"message":"transient"}`, http.StatusBadGateway)
				return
			}
		}
		items := []map[string]any{}
		if r.URL.Query().Get("page") == "1" {
			items = append(items, map[string]any{"id": int64(1000), "full_name": "acme/repo-000", "archived": false})
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	t.Cleanup(server.Close)

	retry := rest.Config{
		MaxAttempts: 4,
		BaseBackoff: time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			waits = append(waits, d)
			return nil
		},
	}
	records, complete, err := listGitHubOrgRepositoriesFromWithRetry(
		context.Background(), server.Client(), server.URL, "acme", 100, "token-1", retry,
	)
	if err != nil {
		t.Fatalf("listGitHubOrgRepositoriesFromWithRetry() error = %v", err)
	}
	if len(records) != 1 || !complete {
		t.Fatalf("listed %d complete %v, want 1 complete true", len(records), complete)
	}
	mu.Lock()
	defer mu.Unlock()
	if pageOneAttempts != 3 {
		t.Fatalf("page-1 attempts = %d, want 3", pageOneAttempts)
	}
	if fmt.Sprint(waits) != fmt.Sprint([]time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("backoff waits = %v, want [1s 2s]", waits)
	}
}

// TestListGitHubOrgRepositoriesGivesUpAfterBoundedRetries pins the other half
// of #8076: a persistent 500 fails the listing after exactly maxAttempts
// requests, and a 401 never retries.
func TestListGitHubOrgRepositoriesGivesUpAfterBoundedRetries(t *testing.T) {
	t.Parallel()

	t.Run("persistent 500 stops after maxAttempts", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			http.Error(w, `{"message":"down"}`, http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)
		retry := rest.Config{MaxAttempts: 3, BaseBackoff: time.Second, Sleep: func(context.Context, time.Duration) error { return nil }}
		if _, complete, err := listGitHubOrgRepositoriesFromWithRetry(
			context.Background(), server.Client(), server.URL, "acme", 100, "token-1", retry,
		); err == nil || complete {
			t.Fatalf("= complete %v, err %v; want an error and incomplete", complete, err)
		}
		if got := attempts.Load(); got != 3 {
			t.Fatalf("attempts = %d, want 3", got)
		}
	})

	t.Run("401 fails without retry", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			http.Error(w, `{"message":"bad credentials"}`, http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)
		var sleeps atomic.Int64
		retry := rest.Config{MaxAttempts: 4, BaseBackoff: time.Second, Sleep: func(context.Context, time.Duration) error { sleeps.Add(1); return nil }}
		if _, _, err := listGitHubOrgRepositoriesFromWithRetry(
			context.Background(), server.Client(), server.URL, "acme", 100, "token-1", retry,
		); err == nil {
			t.Fatal("expected an error for 401, got nil")
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts = %d, want 1", got)
		}
		if got := sleeps.Load(); got != 0 {
			t.Fatalf("sleeps = %d, want 0", got)
		}
	})
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

// TestDefaultGitHubListingRetryBudget pins the production retry budget the
// PR proof quotes (4 attempts, 1s/2s/4s waits): the wrapper tests pass with
// any budget, so without this pin the headline numbers could drift silently.
func TestDefaultGitHubListingRetryBudget(t *testing.T) {
	t.Parallel()
	if got := rest.DefaultConfig().MaxAttempts; got != 4 {
		t.Fatalf("rest.DefaultConfig().MaxAttempts = %d, want 4", got)
	}
	if got := rest.DefaultConfig().BaseBackoff; got != time.Second {
		t.Fatalf("rest.DefaultConfig().BaseBackoff = %v, want 1s", got)
	}
}
