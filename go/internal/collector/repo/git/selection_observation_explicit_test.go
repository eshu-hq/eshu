// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const explicitObservationToken = "ghp_explicitselectiontokenvalue0123"

// explicitConfiguredRepositories spans two owners and a bare name that
// ESHU_GITHUB_ORG qualifies, so the per-owner grouping is exercised.
var explicitConfiguredRepositories = []string{"acme/api", "acme/web", "Other/Svc", "tools"}

func explicitObservationSelector(t *testing.T, shardCount, shardIndex int, observer RepositorySelectionObserver) NativeRepositorySelector {
	t.Helper()
	return NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:                t.TempDir(),
			SourceMode:              "explicit",
			GithubOrg:               "acme",
			GitAuthMethod:           "token",
			GitToken:                explicitObservationToken,
			Repositories:            slices.Clone(explicitConfiguredRepositories),
			RepoShardCount:          shardCount,
			RepoShardIndex:          shardIndex,
			SelectionLivenessWindow: 48 * time.Hour,
		},
		Now: func() time.Time { return time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC) },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, nil
		},
		SelectionObserver: observer,
	}
}

// syncWrittenScope runs the production path a git sync of repoID takes to its
// ingestion scope: the managed checkout path, buildSelectedRepositories, then
// repositoryidentity.MetadataFor and buildScope as processRepository does.
func syncWrittenScope(t *testing.T, config RepoSyncConfig, repoID string) (scopeID, repoSlug string) {
	t.Helper()
	checkoutName, err := repoCheckoutName(repoID)
	if err != nil {
		t.Fatalf("repoCheckoutName(%q) error = %v", repoID, err)
	}
	checkout := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
	selected := buildSelectedRepositories(config, []string{checkout}, nil, nil, nil, nil, nil)
	if len(selected) != 1 {
		t.Fatalf("buildSelectedRepositories(%q) = %d repositories, want 1", repoID, len(selected))
	}
	metadata, err := repositoryidentity.MetadataFor(filepath.Base(selected[0].RepoPath), selected[0].RepoPath, selected[0].RemoteURL)
	if err != nil {
		t.Fatalf("MetadataFor(%q) error = %v", repoID, err)
	}
	written := buildScope(metadata, "")
	return written.ScopeID, written.Metadata["repo_slug"]
}

func TestNativeRepositorySelectorObservesExplicitRepositoriesPerOwnerOnShardZero(t *testing.T) {
	t.Parallel()

	observer := &recordingSelectionObserver{}
	selector := explicitObservationSelector(t, 3, 0, observer)
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(observer.requests) != 2 {
		t.Fatalf("explicit observations = %d, want one per owner (acme, other)", len(observer.requests))
	}
	principal := membership.TokenPrincipal(explicitObservationToken)
	wantSelectors := []membership.Selector{
		membership.NewExplicitSelector("explicit", "acme", []membership.Rule{
			{Kind: "exact", Value: "acme/api"}, {Kind: "exact", Value: "acme/web"}, {Kind: "exact", Value: "tools"},
		}, principal),
		membership.NewExplicitSelector("explicit", "other", []membership.Rule{{Kind: "exact", Value: "Other/Svc"}}, principal),
	}
	wantSlugs := [][]string{{"acme/api", "acme/tools", "acme/web"}, {"other/svc"}}
	for i, request := range observer.requests {
		if request.Selector != wantSelectors[i] {
			t.Fatalf("request %d selector = %+v, want %+v", i, request.Selector, wantSelectors[i])
		}
		if !request.Listing.Complete || request.SourceMode != "explicit" || request.RepoShardCount != 3 || request.LivenessWindow != 48*time.Hour {
			t.Fatalf("request %d = %+v, want a complete explicit listing over 3 shards with the 48h window", i, request)
		}
		if wantSweep := i == len(observer.requests)-1; request.SweepExpired != wantSweep {
			t.Fatalf("request %d SweepExpired = %v, want %v: only the cycle's last request sweeps", i, request.SweepExpired, wantSweep)
		}
		var slugs []string
		for _, listed := range request.Listing.Repositories {
			if listed.State != membership.StateSelected {
				t.Fatalf("request %d lists %s as %q, want only selected", i, listed.Slug, listed.State)
			}
			slugs = append(slugs, listed.Slug)
		}
		slices.Sort(slugs)
		if !slices.Equal(slugs, wantSlugs[i]) {
			t.Fatalf("request %d slugs = %v, want the full pre-shard configured list %v", i, slugs, wantSlugs[i])
		}
	}
}

func TestExplicitSelectionScopeIDsMatchWhatASyncWrites(t *testing.T) {
	t.Parallel()

	observer := &recordingSelectionObserver{}
	selector := explicitObservationSelector(t, 1, 0, observer)
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	bySlug := make(map[string]string)
	for _, request := range observer.requests {
		for _, listed := range request.Listing.Repositories {
			bySlug[listed.Slug] = listed.ScopeID
		}
	}
	for _, repoID := range explicitConfiguredRepositories {
		wantScope, wantSlug := syncWrittenScope(t, selector.Config, repoID)
		if wantScope == "" || bySlug[wantSlug] != wantScope {
			t.Fatalf("configured %s observed scope %q under slug %q, want the scope a sync writes %q", repoID, bySlug[wantSlug], wantSlug, wantScope)
		}
	}
}

func TestNativeRepositorySelectorObservesExplicitOnlyOnShardZero(t *testing.T) {
	t.Parallel()

	for _, index := range []int{1, 2} {
		observer := &recordingSelectionObserver{}
		if _, err := explicitObservationSelector(t, 3, index, observer).SelectRepositories(context.Background()); err != nil {
			t.Fatalf("shard %d SelectRepositories() error = %v", index, err)
		}
		if len(observer.requests) != 0 {
			t.Fatalf("explicit shard %d observations = %d, want 0", index, len(observer.requests))
		}
	}
}

func TestFilesystemModeNeverObservesSelection(t *testing.T) {
	t.Parallel()

	observer := &recordingSelectionObserver{}
	selector := explicitObservationSelector(t, 1, 0, observer)
	selector.Config.SourceMode = "filesystem"
	selector.DiscoverSelection = func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
		return RepositorySelection{RepositoryIDs: slices.Clone(explicitConfiguredRepositories)}, nil
	}
	selector.SyncFilesystem = func(context.Context, RepoSyncConfig, []string) (FilesystemSyncSelection, error) {
		return FilesystemSyncSelection{}, nil
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(observer.requests) != 0 {
		t.Fatalf("filesystem observations = %d, want 0", len(observer.requests))
	}
}

// ownerKnownStore is a membership.Store that returns each owner's known
// scopes and records every host argument and batch it sees.
type ownerKnownStore struct {
	known   map[string][]membership.KnownScope
	hosts   []string
	batches []membership.Batch
}

func (s *ownerKnownStore) KnownScopes(_ context.Context, owner, host string) ([]membership.KnownScope, error) {
	s.hosts = append(s.hosts, host)
	return s.known[owner], nil
}

func (s *ownerKnownStore) Observations(context.Context, string) ([]membership.Observation, error) {
	return nil, nil
}

func (s *ownerKnownStore) UpsertObservations(_ context.Context, batch membership.Batch) error {
	s.batches = append(s.batches, batch)
	return nil
}

func (*ownerKnownStore) DeleteExpiredObservations(context.Context, time.Time, time.Duration) (int64, error) {
	return 0, nil
}

// TestExplicitSelectionWritesSelectedRowsOnlyForScopedRepositories runs the
// real observer: acme/web has no scope yet and gets no row, the unconfigured
// acme/legacy scope is left alone, and every written row is selected.
func TestExplicitSelectionWritesSelectedRowsOnlyForScopedRepositories(t *testing.T) {
	t.Parallel()

	store := &ownerKnownStore{known: map[string][]membership.KnownScope{}}
	selector := explicitObservationSelector(t, 1, 0, membership.Observer{Store: store})
	scoped := map[string]string{}
	for _, repoID := range []string{"acme/api", "tools", "Other/Svc", "acme/legacy"} {
		scopeID, slug := syncWrittenScope(t, selector.Config, repoID)
		owner, _, _ := strings.Cut(slug, "/")
		store.known[owner] = append(store.known[owner], membership.KnownScope{ScopeID: scopeID, Slug: slug})
		scoped[repoID] = scopeID
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	var written []string
	for _, batch := range store.batches {
		if batch.Selector.Kind != membership.KindExplicit {
			t.Fatalf("batch selector kind = %q, want explicit", batch.Selector.Kind)
		}
		for _, row := range batch.Rows {
			if row.State != membership.StateSelected {
				t.Fatalf("explicit row %s state = %q, want only selected", row.ScopeID, row.State)
			}
			written = append(written, row.ScopeID)
		}
	}
	want := []string{scoped["acme/api"], scoped["tools"], scoped["Other/Svc"]}
	slices.Sort(written)
	slices.Sort(want)
	if !slices.Equal(written, want) {
		t.Fatalf("written scopes = %v, want only the configured repositories with scopes %v", written, want)
	}
	for _, host := range store.hosts {
		if host != "" {
			t.Fatalf("explicit KnownScopes host = %q, want no host filter", host)
		}
	}
	if len(store.hosts) == 0 {
		t.Fatal("explicit selection never read known scopes")
	}
}

func TestExplicitSelectionStoreErrorDoesNotFailTheCycle(t *testing.T) {
	t.Parallel()

	selector := explicitObservationSelector(t, 1, 0, membership.Observer{Store: failingSelectionStore{}})
	var synced []string
	selector.SyncGit = func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
		synced = slices.Clone(repositoryIDs)
		return GitSyncSelection{}, nil
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil when the selection store fails", err)
	}
	if len(synced) != len(explicitConfiguredRepositories) {
		t.Fatalf("synced %v after the selection store failed, want all %d configured repositories", synced, len(explicitConfiguredRepositories))
	}
}
