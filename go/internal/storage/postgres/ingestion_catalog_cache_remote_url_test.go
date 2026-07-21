// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// cachedCatalogRemoteURL reads the shared catalog cache without touching the
// database (the cache must be warm) and returns the RemoteURL for repoID, or
// ("", false) when the repo is not cached. Mirrors cachedCatalogAliases for
// the RemoteURL field (issue #5483 C2).
func cachedCatalogRemoteURL(t *testing.T, store IngestionStore, repoID string) (string, bool) {
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
			return entry.RemoteURL, true
		}
	}
	return "", false
}

// catalogRepositoryFactWithRemoteURL builds a repository fact carrying a
// remote_url so RemoteURL-drift tests can mutate identity across commits
// without touching name/repo_slug (isolating the RemoteURL comparison from
// the pre-existing alias comparison).
func catalogRepositoryFactWithRemoteURL(scopeID, generationID, repoID, remoteURL string, observedAt time.Time) facts.Envelope {
	fact := catalogRepositoryFact(scopeID, generationID, repoID, observedAt)
	fact.Payload = map[string]any{"graph_id": repoID, "remote_url": remoteURL}
	return fact
}

// TestIngestionStoreReloadsCatalogWhenKnownRepoRemoteURLDrifts is the #5483 C2
// counterpart to TestIngestionStoreReloadsCatalogWhenKnownRepoAliasDrifts: the
// shared catalog cache must react when an already-known repository's remote
// URL changes, not only when its aliases change. discoverStructuredFluxEvidence
// resolves by STRICT CatalogEntry.RemoteURL equality, so a stale cached URL
// would silently keep failing to link (or link the wrong repository) a Flux
// GitRepository spec.url against this repository until the process restarted.
func TestIngestionStoreReloadsCatalogWhenKnownRepoRemoteURLDrifts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known","remote_url":"https://github.com/myorg/old-mirror-host.git"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	// Commit 1: caches the catalog with the original remote URL.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithRemoteURL(
				"scope-known", "gen-1", "repo-known", "https://github.com/myorg/old-mirror-host.git", now.Add(-time.Minute),
			),
		}),
	); err != nil {
		t.Fatalf("commit 1: CommitScopeGeneration() error = %v, want nil", err)
	}
	if got, ok := cachedCatalogRemoteURL(t, store, "repo-known"); !ok || got != "https://github.com/myorg/old-mirror-host" {
		t.Fatalf("cached RemoteURL after commit 1 = %q (ok=%v), want the normalized original URL", got, ok)
	}

	// The committed generation migrates the remote host. Aliases are
	// deliberately unchanged (same repo_id-derived alias) so this isolates the
	// RemoteURL comparison from the pre-existing alias-drift comparison.
	db.mu.Lock()
	db.catalogPayloads = [][]byte{[]byte(`{"graph_id":"repo-known","remote_url":"https://github.com/myorg/new-mirror-host.git"}`)}
	db.mu.Unlock()

	// Commit 2: same repo id, drifted remote_url. This must merge the fresh
	// RemoteURL into the cache so a later commit's Flux resolution observes it.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-2", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithRemoteURL(
				"scope-known", "gen-2", "repo-known", "https://github.com/myorg/new-mirror-host.git", now.Add(-time.Minute),
			),
		}),
	); err != nil {
		t.Fatalf("commit 2: CommitScopeGeneration() error = %v, want nil", err)
	}

	got, ok := cachedCatalogRemoteURL(t, store, "repo-known")
	if !ok {
		t.Fatal("repo-known missing from cache after RemoteURL drift merge")
	}
	if got != "https://github.com/myorg/new-mirror-host" {
		t.Fatalf("cached RemoteURL after drift = %q, want the migrated normalized URL", got)
	}
}
