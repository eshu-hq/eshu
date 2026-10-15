// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
)

type gitlabAPI struct {
	t            *testing.T
	server       *httptest.Server
	pages        map[int]string
	mu           sync.Mutex
	projectHits  int
	resolveGroup string
}

// projectHitCount returns the number of /projects requests. The handler
// runs on server goroutines, so tests read under the same mutex.
func (f *gitlabAPI) projectHitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.projectHits
}

func newGitLabAPI(t *testing.T) *gitlabAPI {
	t.Helper()
	f := &gitlabAPI{t: t, pages: map[int]string{}, resolveGroup: `{"id":42,"full_path":"acme/platform"}`}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.EscapedPath(), "/projects") {
			f.mu.Lock()
			f.projectHits++
			f.mu.Unlock()
			page := 0
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			body, ok := f.pages[page]
			if !ok {
				body = "[]"
			}
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, f.resolveGroup)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *gitlabAPI) clientFactory() func(string, string) (*gitlab.Client, error) {
	return func(_, token string) (*gitlab.Client, error) {
		return gitlab.NewClient(f.server.Client(), f.server.URL, token, nil, nil)
	}
}

func gitlabProject(id int, path string, archived bool) string {
	return fmt.Sprintf(`{"id":%d,"path_with_namespace":%q,"archived":%v}`, id, path, archived)
}

func gitlabDiscoveryConfig() RepoSyncConfig {
	return RepoSyncConfig{
		ReposDir:    "/tmp/eshu-gitlab-test-repos",
		SourceMode:  "gitlabGroup",
		GitlabHost:  "gitlab.com",
		GitlabGroup: "acme/platform",
		RepoLimit:   4000,
	}
}

// TestDiscoverSelectionGitLabGroup proves the discovery wiring: resolve,
// list, gitlab/ IDs, archived and rule classification, completeness, and
// the resolved group path.
func TestDiscoverSelectionGitLabGroup(t *testing.T) {
	t.Parallel()

	api := newGitLabAPI(t)
	api.pages[1] = "[" + strings.Join([]string{
		gitlabProject(101, "acme/platform/a", false),
		gitlabProject(102, "acme/platform/b", true),
		gitlabProject(103, "acme/platform/c", false),
	}, ",") + "]"
	config := gitlabDiscoveryConfig()
	config.RepositoryRules = []RepoSyncRepositoryRule{{Kind: "regex", Value: `^gitlab/acme/platform/(a|b)`}}

	selection, err := discoverSelection(context.Background(), config, "glpat-test", api.clientFactory())
	if err != nil {
		t.Fatalf("discoverSelection() error = %v", err)
	}
	if len(selection.RepositoryIDs) != 1 || selection.RepositoryIDs[0] != "gitlab/acme/platform/a" {
		t.Fatalf("RepositoryIDs = %v, want [gitlab/acme/platform/a]", selection.RepositoryIDs)
	}
	if len(selection.ArchivedRepositoryIDs) != 1 || selection.ArchivedRepositoryIDs[0] != "gitlab/acme/platform/b" {
		t.Fatalf("ArchivedRepositoryIDs = %v, want [gitlab/acme/platform/b]", selection.ArchivedRepositoryIDs)
	}
	if len(selection.RuleExcludedRepositoryIDs) != 1 || selection.RuleExcludedRepositoryIDs[0] != "gitlab/acme/platform/c" {
		t.Fatalf("RuleExcludedRepositoryIDs = %v, want [gitlab/acme/platform/c]", selection.RuleExcludedRepositoryIDs)
	}
	if len(selection.ListedRepositories) != 3 || !selection.ListingComplete {
		t.Fatalf("listed = %d complete = %v, want 3 and true", len(selection.ListedRepositories), selection.ListingComplete)
	}
	if selection.GroupFullPath != "acme/platform" {
		t.Fatalf("GroupFullPath = %q, want acme/platform", selection.GroupFullPath)
	}
	if api.projectHitCount() != 2 {
		t.Fatalf("project requests = %d, want 2 (one page plus the empty page)", api.projectHitCount())
	}
}

func TestDiscoverSelectionGitLabGroupValidation(t *testing.T) {
	t.Parallel()

	api := newGitLabAPI(t)
	config := gitlabDiscoveryConfig()

	for _, tc := range []struct {
		name   string
		mutate func(*RepoSyncConfig, *string)
		want   string
	}{
		{"missing group", func(c *RepoSyncConfig, token *string) { c.GitlabGroup = "" }, "ESHU_GITLAB_GROUP"},
		{"missing token", func(c *RepoSyncConfig, token *string) { *token = "" }, "ESHU_GIT_TOKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := "glpat-test"
			cfg := config
			tc.mutate(&cfg, &token)
			if _, err := discoverSelection(context.Background(), cfg, token, api.clientFactory()); err == nil {
				t.Fatal("discoverSelection() error = nil, want non-nil")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("discoverSelection() error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestDiscoverSelectionGitLabGroupRejectsExactRuleOutsideResolvedGroup
// proves the authoritative prefix check for a numeric group: it runs after
// ResolveGroup against the resolved full_path, before any /projects
// request, and writes nothing.
func TestDiscoverSelectionGitLabGroupRejectsExactRuleOutsideResolvedGroup(t *testing.T) {
	t.Parallel()

	api := newGitLabAPI(t)
	config := gitlabDiscoveryConfig()
	config.GitlabGroup = "42"
	config.RepositoryRules = []RepoSyncRepositoryRule{{Kind: "exact", Value: "gitlab/acme/other/x"}}

	_, err := discoverSelection(context.Background(), config, "glpat-test", api.clientFactory())
	if err == nil {
		t.Fatal("discoverSelection() error = nil, want config_invalid")
	}
	if got := gitlab.ClassOf(err); got != gitlab.FailureClassConfigInvalid {
		t.Fatalf("failure class = %q, want config_invalid", got)
	}
	if api.projectHitCount() != 0 {
		t.Fatalf("project requests = %d, want 0 (fail before the listing)", api.projectHitCount())
	}
}

// TestGitLabGroupSelectionRequestMapsScopeIdentity proves listed projects
// map to the scope IDs and slugs a git sync of them would write, with the
// configured host.
func TestGitLabGroupSelectionRequestMapsScopeIdentity(t *testing.T) {
	t.Parallel()

	config := gitlabDiscoveryConfig()
	config.RepositoryRules = []RepoSyncRepositoryRule{{Kind: "regex", Value: `^gitlab/acme/platform/`}}
	discovered := RepositorySelection{
		RepositoryIDs:             []string{"gitlab/acme/platform/a"},
		ListedRepositories:        []GitHubRepositoryRecord{{RepoID: "gitlab/acme/platform/a", GitHubID: 101}},
		ArchivedRepositoryIDs:     []string{"gitlab/acme/platform/b"},
		RuleExcludedRepositoryIDs: []string{"gitlab/acme/platform/c"},
		ListingComplete:           true,
		GroupFullPath:             "acme/platform",
	}
	// The archived and excluded IDs must also be listed records for the
	// request to classify them.
	discovered.ListedRepositories = append(discovered.ListedRepositories,
		GitHubRepositoryRecord{RepoID: "gitlab/acme/platform/b", GitHubID: 102, Archived: true},
		GitHubRepositoryRecord{RepoID: "gitlab/acme/platform/c", GitHubID: 103},
	)
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)

	request := gitlabGroupSelectionRequest(config, discovered, now)

	if request.Selector.Kind != membership.KindGitLabGroup {
		t.Fatalf("selector kind = %q, want gitlab_group", request.Selector.Kind)
	}
	if request.Selector.Host != "gitlab.com" || request.Selector.Owner != "acme/platform" {
		t.Fatalf("selector = %+v, want host gitlab.com and owner acme/platform", request.Selector)
	}
	if !request.Listing.Complete || !request.SweepExpired {
		t.Fatalf("request = %+v, want a complete listing carrying the cycle's sweep", request)
	}
	byScope := map[string]string{}
	wantState := map[string]membership.State{
		"gitlab/acme/platform/a": membership.StateSelected,
		"gitlab/acme/platform/b": membership.StateArchivedExcluded,
		"gitlab/acme/platform/c": membership.StateRuleExcluded,
	}
	for repoID := range wantState {
		byScope[gitScopeIDForRepositoryID(config, repoID)] = repoID
	}
	if len(request.Listing.Repositories) != 3 {
		t.Fatalf("listed = %d, want 3", len(request.Listing.Repositories))
	}
	for _, record := range request.Listing.Repositories {
		repoID, ok := byScope[record.ScopeID]
		if !ok {
			t.Fatalf("record scope %q matches no listed repository", record.ScopeID)
		}
		wantScope, wantSlug := gitScopeIdentityForRepositoryID(config, repoID)
		if record.ScopeID != wantScope || record.Slug != wantSlug {
			t.Fatalf("record = %+v, want scope %q slug %q", record, wantScope, wantSlug)
		}
		if record.State != wantState[repoID] {
			t.Fatalf("%s state = %q, want %q", repoID, record.State, wantState[repoID])
		}
		if repoID == "gitlab/acme/platform/a" && record.GitHubID != 101 {
			t.Fatalf("a provider id = %d, want 101", record.GitHubID)
		}
	}
}

// TestObserveSelectionGitLabGroupShardZeroOnly proves gitlabGroup observes
// on shard 0 only, with a gitlab_group selector carrying the resolved
// group path.
func TestObserveSelectionGitLabGroupShardZeroOnly(t *testing.T) {
	t.Parallel()

	discovered := RepositorySelection{
		RepositoryIDs:      []string{"gitlab/acme/platform/a"},
		ListedRepositories: []GitHubRepositoryRecord{{RepoID: "gitlab/acme/platform/a", GitHubID: 101}},
		ListingComplete:    true,
		GroupFullPath:      "acme/platform",
	}
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	newSelector := func(index int, observer *recordingSelectionObserver) NativeRepositorySelector {
		return NativeRepositorySelector{
			Config: RepoSyncConfig{
				ReposDir: t.TempDir(), SourceMode: "gitlabGroup", GitlabGroup: "acme/platform",
				RepoLimit: 4000, RepoShardCount: 2, RepoShardIndex: index,
			},
			SelectionObserver: observer,
		}
	}

	observer := &recordingSelectionObserver{}
	newSelector(0, observer).observeSelection(context.Background(), discovered, now)
	if len(observer.requests) != 1 {
		t.Fatalf("shard 0 observations = %d, want 1", len(observer.requests))
	}
	request := observer.requests[0]
	if request.Selector.Kind != membership.KindGitLabGroup || request.Selector.Owner != "acme/platform" {
		t.Fatalf("selector = %+v, want the gitlab_group selector for acme/platform", request.Selector)
	}
	if !request.SweepExpired {
		t.Fatal("the gitlabGroup request must carry the cycle's one sweep")
	}

	other := &recordingSelectionObserver{}
	newSelector(1, other).observeSelection(context.Background(), discovered, now)
	if len(other.requests) != 0 {
		t.Fatalf("shard 1 observations = %d, want 0", len(other.requests))
	}
}

// BenchmarkGitLabGroupSelectionRequest measures building the observation
// request for an 800-project group listing, dominated by one scope id
// derivation per listed project, beside BenchmarkGitHubOrgSelectionRequest.
func BenchmarkGitLabGroupSelectionRequest(b *testing.B) {
	records := make([]GitHubRepositoryRecord, 0, 800)
	for i := range 800 {
		records = append(records, GitHubRepositoryRecord{RepoID: fmt.Sprintf("gitlab/acme/platform/repo-%03d", i), GitHubID: int64(i + 1)})
	}
	config := RepoSyncConfig{ReposDir: b.TempDir(), SourceMode: "gitlabGroup", GitlabGroup: "acme/platform", GitlabHost: "gitlab.com", RepoLimit: 4000}
	discovered := RepositorySelection{
		ListedRepositories: records,
		ListingComplete:    true,
		GroupFullPath:      "acme/platform",
	}
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		if request := gitlabGroupSelectionRequest(config, discovered, now); len(request.Listing.Repositories) != 800 {
			b.Fatalf("listed = %d, want 800", len(request.Listing.Repositories))
		}
	}
}

// TestGitLabGroupHostLockstep proves the API base, the selector host, the
// clone remote, and the scope identity all read one host.
func TestGitLabGroupHostLockstep(t *testing.T) {
	t.Parallel()

	api := newGitLabAPI(t)
	var apiBase, tokenSeen string
	factory := func(base, token string) (*gitlab.Client, error) {
		apiBase, tokenSeen = base, token
		return api.clientFactory()(base, token)
	}
	config := gitlabDiscoveryConfig()
	config.GitlabHost = "gitlab.corp.example"

	if _, err := discoverSelection(context.Background(), config, "glpat-test", factory); err != nil {
		t.Fatalf("discoverSelection() error = %v", err)
	}
	if apiBase != "https://gitlab.corp.example/api/v4" {
		t.Fatalf("apiBase = %q, want the configured host", apiBase)
	}
	if tokenSeen != "glpat-test" {
		t.Fatalf("token = %q, want the configured token", tokenSeen)
	}
	if got := repoRemoteURL(config, "gitlab/acme/platform/a"); got != "https://gitlab.corp.example/acme/platform/a.git" {
		t.Fatalf("repoRemoteURL() = %q, want the configured host", got)
	}
	custom := gitScopeIDForRepositoryID(config, "gitlab/acme/platform/a")
	config.GitlabHost = "gitlab.com"
	if plain := gitScopeIDForRepositoryID(config, "gitlab/acme/platform/a"); plain == custom {
		t.Fatalf("scope id %q is host-independent, want the host hashed", custom)
	}
}
