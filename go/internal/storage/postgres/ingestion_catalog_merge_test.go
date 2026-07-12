// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// cachedCatalogAliases reads the shared catalog cache without touching the
// database (the cache must be warm) and returns the alias list for repoID,
// or nil when the repo is not cached. It lets merge tests assert cache
// CONTENT — the accuracy contract — rather than only load counts.
func cachedCatalogAliases(t *testing.T, store IngestionStore, repoID string) []string {
	t.Helper()
	snap, err := store.repositoryCatalog(context.Background(), nil)
	if err != nil {
		t.Fatalf("read cached catalog: %v", err)
	}
	if !snap.CacheHit {
		t.Fatal("catalog cache was cold; merge tests require a warm cache")
	}
	for _, entry := range snap.Entries {
		if entry.RepoID == repoID {
			return entry.Aliases
		}
	}
	return nil
}

// TestIngestionStoreMergesCatalogInsteadOfReloadingOnNewRepository is the
// #5129 regression test. Bootstrap commits introduce a new repository on
// every generation; the pre-fix cache evicted wholesale on each one, forcing
// the next commit to reload the entire catalog (measured 382.6s of strictly
// serialized commit-chain time on the accepted 896-repo run — see #5122).
// The fix merges the committed generation's repository identities into the
// cached catalog instead, so onboarding commits stop paying per-commit
// reloads while later commits still observe the new repository.
func TestIngestionStoreMergesCatalogInsteadOfReloadingOnNewRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	// Commit 1: known repo — cold cache loads once.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-known", "gen-1", "repo-known", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 1: CommitScopeGeneration() error = %v, want nil", err)
	}
	sharedLoadsAfterWarm := db.catalogQueries

	// Commits 2..4: each introduces a brand-new repository (bootstrap shape).
	for i, repoID := range []string{"repo-new-a", "repo-new-b", "repo-new-c"} {
		db.mu.Lock()
		db.catalogPayloads = append(db.catalogPayloads, []byte(`{"graph_id":"`+repoID+`"}`))
		db.mu.Unlock()
		genID := "gen-new-" + string(rune('a'+i))
		if err := store.CommitScopeGeneration(
			context.Background(),
			catalogTestScope("scope-"+repoID, repoID),
			catalogTestGeneration("scope-"+repoID, genID, now),
			testFactChannel([]facts.Envelope{
				catalogRepositoryFact("scope-"+repoID, genID, repoID, now.Add(-time.Minute)),
			}),
		); err != nil {
			t.Fatalf("new-repo commit %s: CommitScopeGeneration() error = %v, want nil", repoID, err)
		}
	}

	// Commit 5: known repo again — must reuse the merged cache, not reload.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-final", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-known", "gen-final", "repo-known", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("final commit: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Accuracy first: every onboarded repo is visible in the cached catalog.
	for _, repoID := range []string{"repo-known", "repo-new-a", "repo-new-b", "repo-new-c"} {
		if aliases := cachedCatalogAliases(t, store, repoID); aliases == nil {
			t.Fatalf("repo %s missing from merged catalog cache", repoID)
		}
	}

	// Performance: the SHARED cache never reloads after the initial fill. The
	// only additional loads come from the per-commit relationship backfill's
	// own deliberately uncached reads for newly onboarded repos (#4451 § T8),
	// which this store performs once per new repo. Shared-cache loads staying
	// at the warm-fill count is the O(1) contract under bootstrap onboarding;
	// pre-fix this was one extra shared reload per new-repo commit.
	backfillLoads := 3 // one per onboarded repo (repo-new-a/b/c)
	if got := db.catalogQueries; got != sharedLoadsAfterWarm+backfillLoads {
		t.Fatalf(
			"catalog loads = %d after 3 new-repo commits, want %d (warm fill %d + %d backfill-only reloads, zero shared-cache reloads)",
			got, sharedLoadsAfterWarm+backfillLoads, sharedLoadsAfterWarm, backfillLoads,
		)
	}
}

// TestIngestionStoreMergeIgnoresStaleGenerationIdentity pins the freshness
// contract raised in PR #5134 review: a replayed/reconciled OLDER generation
// (dead-letter replay commits an earlier observed_at repository fact after a
// newer identity is already cached) must NOT regress the cached aliases. A
// fresh reload keeps the newest row (ORDER BY observed_at DESC); the merge
// must honor the same freshness key and skip the stale identity.
func TestIngestionStoreMergeIgnoresStaleGenerationIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known","repo_slug":"new-slug"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	// Commit 1: current identity (new-slug), observed now-1m. Warm cache.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-current", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-current", "repo-known", "new-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("current commit: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Commit 2: dead-letter replay of an OLDER generation carrying the stale
	// slug, observed an hour earlier. The durable table keeps both rows and a
	// reload would still pick new-slug; the merged cache must too.
	staleFact := catalogRepositoryFactWithSlug(
		"scope-known", "gen-stale-replay", "repo-known", "old-slug", now.Add(-time.Hour),
	)
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-stale-replay", now),
		testFactChannel([]facts.Envelope{staleFact}),
	); err != nil {
		t.Fatalf("stale replay commit: CommitScopeGeneration() error = %v, want nil", err)
	}

	aliases := cachedCatalogAliases(t, store, "repo-known")
	hasNew := false
	for _, a := range aliases {
		if a == "new-slug" {
			hasNew = true
		}
		if a == "old-slug" {
			t.Fatalf("stale replay regressed cached aliases to %q: %v (reload would keep new-slug)", a, aliases)
		}
	}
	if !hasNew {
		t.Fatalf("cached catalog lost current alias new-slug after stale replay: %v", aliases)
	}
}

// TestIngestionStoreMergesAliasDriftWithoutReload pins the #3521 P2 accuracy
// contract under the #5129 merge: when a known repository's identity aliases
// drift (slug rename), the cached entry is REPLACED in place — the next
// commit observes the new alias — without paying a full catalog reload.
func TestIngestionStoreMergesAliasDriftWithoutReload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known","repo_slug":"old-slug"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	// Commit 1: caches the catalog with alias "old-slug".
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-1", "repo-known", "old-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 1: CommitScopeGeneration() error = %v, want nil", err)
	}
	loadsAfterWarm := db.catalogQueries

	// Commit 2: same repo id, drifted slug — merge must replace the cached
	// entry so the drift is visible without a reload.
	db.mu.Lock()
	db.catalogPayloads = [][]byte{[]byte(`{"graph_id":"repo-known","repo_slug":"new-slug"}`)}
	db.mu.Unlock()
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-2", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-2", "repo-known", "new-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 2: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Accuracy: the cached aliases now carry the drifted slug (#3521 P2 —
	// DiscoverEvidence matches via aliases; a stale alias silently drops
	// cross-repo evidence for the renamed repository).
	aliases := cachedCatalogAliases(t, store, "repo-known")
	found := false
	for _, a := range aliases {
		if a == "new-slug" {
			found = true
		}
		if a == "old-slug" {
			t.Fatalf("cached catalog still carries stale alias %q after drift merge: %v", a, aliases)
		}
	}
	if !found {
		t.Fatalf("cached catalog missing drifted alias new-slug: %v", aliases)
	}

	// Performance: no shared-cache reload for the drift (pre-fix: eviction +
	// one full reload). repo-known is not newly onboarded, so no backfill
	// read either.
	if got := db.catalogQueries; got != loadsAfterWarm {
		t.Fatalf("catalog loads = %d after alias drift, want %d (merge, not reload)", got, loadsAfterWarm)
	}
}
