// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// repositoryCatalogCache holds the global repository identity catalog so the
// ingestion commit hot path does not reload every repository fact on every
// scope generation commit (issue #3481).
//
// One cache instance is created per IngestionStore (see NewIngestionStore) and
// is shared, via the store's value copies, across that process's concurrent
// commit goroutines. It is not shared across processes; cross-process catalog
// completeness is handled by the deferred BackfillAllRelationshipEvidence pass,
// which reloads the full catalog independently of this cache.
//
// Before this cache the durable commit boundary ran an unbounded
// `SELECT payload FROM fact_records WHERE fact_kind = 'repository'` inside every
// transaction, making onboarding and per-commit cost O(all repositories). The
// catalog only carries repository identity (RepoID plus aliases) and changes
// solely when a repository-identity fact is committed, so it is loaded once
// and kept current by merging each committed generation's repository
// identities in place (#5129); it never reloads while the process runs.
//
// A single IngestionStore value is shared (by interface copy) across concurrent
// collector commit goroutines, so the cache guards its state with a mutex. The
// mutex only protects the in-memory snapshot and a single catalog load; it never
// spans the per-commit Postgres transaction, so it adds no write serialization.
type repositoryCatalogCache struct {
	mu      sync.Mutex
	loaded  bool
	entries []relationships.CatalogEntry
	// entryByID indexes the cached catalog by repository id. It supports both
	// new-repo detection and alias-drift detection (issue #3521) without
	// rescanning the slice, and its key set is the cached repository id set.
	entryByID map[string]relationships.CatalogEntry

	// loads and hits are operator-facing counters for the cache effectiveness
	// on the commit hot path. They are read for structured logging, so they use
	// atomics to stay race-free outside the cache mutex.
	loads atomic.Int64
	hits  atomic.Int64
}

// newRepositoryCatalogCache constructs an empty, unloaded catalog cache.
func newRepositoryCatalogCache() *repositoryCatalogCache {
	return &repositoryCatalogCache{}
}

// catalogSnapshot is the immutable view a single commit reads from the cache.
// Sharing the slice and set is safe because the cache never mutates a published
// snapshot in place; invalidation swaps in freshly built values instead.
type catalogSnapshot struct {
	Entries []relationships.CatalogEntry
	RepoIDs map[string]struct{}
	// CacheHit reports whether the snapshot came from the in-memory cache
	// (true) or required a fresh load (false). Operators use this on the
	// commit stage log to confirm the hot path is not reloading per commit.
	CacheHit bool
}

// get returns the cached repository catalog, loading it once via the supplied
// queryer when the cache is cold. Concurrent callers during a cold cache share
// the single load because the mutex is held across the load.
//
// The caller passes the open ingestion transaction as the queryer so the cold
// load reuses that transaction's connection instead of acquiring a second pool
// connection while the tx is open (issue #3521 P1: a second acquisition can
// deadlock under a saturated or single-connection pool).
func (c *repositoryCatalogCache) get(
	ctx context.Context,
	queryer Queryer,
) (catalogSnapshot, error) {
	if c == nil {
		entries, err := loadRepositoryCatalog(ctx, queryer)
		if err != nil {
			return catalogSnapshot{}, err
		}
		return catalogSnapshot{Entries: entries, RepoIDs: catalogRepoIDs(entries)}, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loaded {
		c.hits.Add(1)
		return catalogSnapshot{Entries: c.entries, RepoIDs: c.snapshotRepoIDsLocked(), CacheHit: true}, nil
	}

	entries, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		return catalogSnapshot{}, err
	}
	c.entries = entries
	c.entryByID = catalogEntryByID(entries)
	c.loaded = true
	c.loads.Add(1)

	return catalogSnapshot{Entries: c.entries, RepoIDs: c.snapshotRepoIDsLocked(), CacheHit: false}, nil
}

// snapshotRepoIDsLocked returns the cached repository id set. The caller holds
// c.mu. The returned map is freshly built and never mutated after return, so
// readers holding it are unaffected by later invalidation.
func (c *repositoryCatalogCache) snapshotRepoIDsLocked() map[string]struct{} {
	ids := make(map[string]struct{}, len(c.entryByID))
	for repoID := range c.entryByID {
		ids[repoID] = struct{}{}
	}
	return ids
}

// mergeChangedRepositories merges a committed generation's repository
// identities into the cached catalog: a repository id the cache had not seen
// is added, and a known repository whose identity aliases drifted (slug/name
// rename) has its entry replaced. DiscoverEvidence matches via
// CatalogEntry.Aliases, so a stale alias would silently drop cross-repo
// evidence for the renamed repository (issue #3521 P2). It returns true when
// the cache changed. Generations over known repositories with unchanged
// identity leave the cache untouched, which is the steady-state hot path.
//
// Merging replaces the pre-#5129 whole-cache eviction: during bootstrap every
// scope onboards a new repository, so eviction forced a full catalog reload
// on every commit — 382.6s of strictly serialized commit-chain time on the
// accepted 896-repo run (#5122). The committed entry and a reloaded row
// derive from the same payload through relationships.RepositoryCatalogEntry
// (#4394 T2), and reload's newest-wins dedup matches replace-on-drift, so the
// merged catalog is set-identical to a fresh reload (equivalence proof
// recorded on #5122 and pinned by TestIngestionStoreMerges*).
//
// Cross-process visibility is unchanged: under eviction the cache reloaded
// only when THIS process onboarded or renamed a repository, so other
// processes' commits were never a reload trigger; corpus-wide catalog
// completeness remains owned by the deferred BackfillAllRelationshipEvidence
// pass, which always reloads fresh.
//
// Published snapshots stay immutable: the merge builds fresh slice and index
// values and swaps them in under the mutex; readers holding an earlier
// snapshot are unaffected.
func (c *repositoryCatalogCache) mergeChangedRepositories(
	currentGenerationRepos map[string]relationships.CatalogEntry,
) bool {
	if c == nil || len(currentGenerationRepos) == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		return false
	}

	changed := false
	for repoID, committed := range currentGenerationRepos {
		cached, known := c.entryByID[repoID]
		if !known || !catalogAliasesEqual(cached.Aliases, committed.Aliases) {
			changed = true
			break
		}
	}
	if !changed {
		return false
	}

	merged := make([]relationships.CatalogEntry, 0, len(c.entries)+len(currentGenerationRepos))
	mergedByID := make(map[string]relationships.CatalogEntry, len(c.entryByID)+len(currentGenerationRepos))
	// New repositories first: reload orders newest-first (observed_at DESC)
	// and the committed generation is the newest identity source. Matcher
	// results are order-insensitive as a set (proof on #5122); this ordering
	// just keeps the slice shape close to what a reload would produce.
	for repoID, committed := range currentGenerationRepos {
		if _, known := c.entryByID[repoID]; !known {
			merged = append(merged, committed)
			mergedByID[repoID] = committed
		}
	}
	for _, entry := range c.entries {
		if replacement, fromGeneration := currentGenerationRepos[entry.RepoID]; fromGeneration {
			merged = append(merged, replacement)
			mergedByID[entry.RepoID] = replacement
			continue
		}
		merged = append(merged, entry)
		mergedByID[entry.RepoID] = entry
	}
	c.entries = merged
	c.entryByID = mergedByID
	return true
}

// catalogEntryByID indexes catalog entries by repository id. loadRepositoryCatalog
// already deduplicates by repo id (newest observed_at wins), so each id appears
// once and the index is unambiguous.
func catalogEntryByID(entries []relationships.CatalogEntry) map[string]relationships.CatalogEntry {
	index := make(map[string]relationships.CatalogEntry, len(entries))
	for _, entry := range entries {
		index[entry.RepoID] = entry
	}
	return index
}

// catalogAliasesEqual reports whether two alias lists describe the same identity
// set. Order is ignored because uniqueCatalogAliases builds the list from a
// fixed field order, but the comparison is set-based so a reordered or
// duplicate-collapsed list does not trigger a spurious reload.
func catalogAliasesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(a))
	for _, alias := range a {
		seen[alias] = struct{}{}
	}
	for _, alias := range b {
		if _, ok := seen[alias]; !ok {
			return false
		}
	}
	return true
}

// loadCount reports how many fresh catalog loads the cache has performed. It
// exists for hot-path commit-stage logging and tests that assert O(1) loads
// across many commits.
func (c *repositoryCatalogCache) loadCount() int64 {
	if c == nil {
		return 0
	}
	return c.loads.Load()
}
