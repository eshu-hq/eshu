// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

type recordingSelectionObserver struct {
	requests []membership.Request
}

func (o *recordingSelectionObserver) Observe(_ context.Context, request membership.Request) membership.Result {
	o.requests = append(o.requests, request)
	return membership.Result{Outcome: membership.OutcomeEvaluated}
}

func githubOrgObservationSelector(t *testing.T, shardCount, shardIndex int, observer RepositorySelectionObserver) NativeRepositorySelector {
	t.Helper()
	return NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       t.TempDir(),
			SourceMode:     "githubOrg",
			GithubOrg:      "acme",
			RepoLimit:      4000,
			RepoShardCount: shardCount,
			RepoShardIndex: shardIndex,
			RepositoryRules: []RepoSyncRepositoryRule{
				{Kind: "regex", Value: "^acme/(api|old)"},
			},
		},
		Now: func() time.Time { return time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC) },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return selectGitHubRepositoryIDs([]GitHubRepositoryRecord{
				{RepoID: "acme/api", GitHubID: 1},
				{RepoID: "acme/api-two", GitHubID: 2},
				{RepoID: "acme/web", GitHubID: 3},
				{RepoID: "acme/old", GitHubID: 4, Archived: true},
			}, []RepoSyncRepositoryRule{{Kind: "regex", Value: "^acme/(api|old)"}}, false), nil
		},
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, nil
		},
		SelectionObserver: observer,
	}
}

func TestNativeRepositorySelectorObservesTheFullListingOnShardZero(t *testing.T) {
	t.Parallel()

	observer := &recordingSelectionObserver{}
	selector := githubOrgObservationSelector(t, 3, 0, observer)
	selector.DiscoverSelection = func(ctx context.Context, config RepoSyncConfig, token string) (RepositorySelection, error) {
		discovered, err := githubOrgObservationSelector(t, 3, 0, nil).DiscoverSelection(ctx, config, token)
		discovered.ListingComplete = true
		return discovered, err
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(observer.requests) != 1 {
		t.Fatalf("observations = %d, want 1 on shard 0", len(observer.requests))
	}
	request := observer.requests[0]
	if !request.Listing.Complete || request.RepoShardCount != 3 || request.RepoLimit != 4000 || request.SourceMode != "githubOrg" {
		t.Fatalf("request = %+v, want complete listing, 3 shards, repo limit 4000, githubOrg", request)
	}
	wantSelector := membership.NewGitHubOrgSelector("githubOrg", "acme", []membership.Rule{{Kind: "regex", Value: "^acme/(api|old)"}}, false, "anonymous")
	if request.Selector != wantSelector {
		t.Fatalf("selector = %+v, want %+v", request.Selector, wantSelector)
	}
	want := map[string]membership.State{
		"acme/api":     membership.StateSelected,
		"acme/api-two": membership.StateSelected,
		"acme/web":     membership.StateRuleExcluded,
		"acme/old":     membership.StateArchivedExcluded,
	}
	if got := len(request.Listing.Repositories); got != len(want) {
		t.Fatalf("listed = %d, want the full pre-shard listing of %d", got, len(want))
	}
	for _, listed := range request.Listing.Repositories {
		if listed.State != want[listed.Slug] {
			t.Fatalf("listed %s state = %q, want %q", listed.Slug, listed.State, want[listed.Slug])
		}
		repoPath := filepath.Join(selector.Config.ReposDir, filepath.FromSlash(listed.Slug))
		if wantScope := gitScopeIDForManagedRepo(selector.Config, repoPath); listed.ScopeID == "" || listed.ScopeID != wantScope {
			t.Fatalf("listed %s scope id = %q, want the checkout derivation %q", listed.Slug, listed.ScopeID, wantScope)
		}
		if listed.GitHubID == 0 {
			t.Fatalf("listed %s carries no GitHub id", listed.Slug)
		}
	}
}

func TestNativeRepositorySelectorObservesOnlyOnShardZeroInGitHubOrgMode(t *testing.T) {
	t.Parallel()

	for _, index := range []int{1, 2} {
		observer := &recordingSelectionObserver{}
		if _, err := githubOrgObservationSelector(t, 3, index, observer).SelectRepositories(context.Background()); err != nil {
			t.Fatalf("shard %d SelectRepositories() error = %v", index, err)
		}
		if len(observer.requests) != 0 {
			t.Fatalf("shard %d observations = %d, want 0", index, len(observer.requests))
		}
	}

	unsharded := &recordingSelectionObserver{}
	if _, err := githubOrgObservationSelector(t, 1, 0, unsharded).SelectRepositories(context.Background()); err != nil {
		t.Fatalf("unsharded SelectRepositories() error = %v", err)
	}
	if len(unsharded.requests) != 1 {
		t.Fatalf("unsharded observations = %d, want 1", len(unsharded.requests))
	}

	explicit := &recordingSelectionObserver{}
	selector := githubOrgObservationSelector(t, 1, 0, explicit)
	selector.Config.SourceMode = "explicit"
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("explicit SelectRepositories() error = %v", err)
	}
	if len(explicit.requests) != 0 {
		t.Fatalf("explicit-mode observations = %d, want 0", len(explicit.requests))
	}
}

// TestSelectorNormalizesExactRulesLikeMatching pins membership's copy of
// normalizeRepositoryID to this package's: an exact rule hashes to the same
// selector id whether or not the collector already normalized its value.
func TestSelectorNormalizesExactRulesLikeMatching(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"acme/web", " /acme//web/ ", `acme\web`, "./acme/./web", "acme/../web", "Acme/Web", "", "  ", "/"} {
		got := membership.NewGitHubOrgSelector("githubOrg", "acme", []membership.Rule{{Kind: "exact", Value: raw}}, false, "")
		want := membership.NewGitHubOrgSelector("githubOrg", "acme", []membership.Rule{{Kind: "exact", Value: normalizeRepositoryID(raw)}}, false, "")
		if got.ID != want.ID {
			t.Fatalf("exact rule %q hashes to %s, want %s (the collector-normalized value)", raw, got.ID, want.ID)
		}
	}
}

func TestWebhookTriggerSelectorCarriesNoSelectionObserver(t *testing.T) {
	t.Parallel()

	observerType := reflect.TypeFor[RepositorySelectionObserver]()
	selectorType := reflect.TypeFor[WebhookTriggerRepositorySelector]()
	for i := range selectorType.NumField() {
		field := selectorType.Field(i)
		if field.Type == observerType || field.Type.Implements(observerType) {
			t.Fatalf("WebhookTriggerRepositorySelector.%s can evaluate selection; only the native githubOrg listing may", field.Name)
		}
	}
}

type failingSelectionStore struct{}

func (failingSelectionStore) KnownScopes(context.Context, string) ([]membership.KnownScope, error) {
	return nil, errors.New("postgres unavailable")
}

func (failingSelectionStore) Observations(context.Context, string) ([]membership.Observation, error) {
	return nil, errors.New("postgres unavailable")
}

func (failingSelectionStore) UpsertObservations(context.Context, membership.Batch) error {
	return errors.New("postgres unavailable")
}

func TestNativeRepositorySelectorSelectionStoreErrorDoesNotFailTheCycle(t *testing.T) {
	t.Parallel()

	selector := githubOrgObservationSelector(t, 1, 0, membership.Observer{Store: failingSelectionStore{}})
	discover := selector.DiscoverSelection
	selector.DiscoverSelection = func(ctx context.Context, config RepoSyncConfig, token string) (RepositorySelection, error) {
		discovered, err := discover(ctx, config, token)
		discovered.ListingComplete = true
		return discovered, err
	}
	synced := false
	selector.SyncGit = func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
		synced = len(repositoryIDs) == 2
		return GitSyncSelection{}, nil
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil when the selection store fails", err)
	}
	if !synced {
		t.Fatal("the cycle did not sync the two selected repositories after the selection store failed")
	}
}
