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
// solely when a repository-identity fact is committed, so it is safe to load
// once and reuse until a commit introduces a repository the cache has not seen.
//
// A single IngestionStore value is shared (by interface copy) across concurrent
// collector commit goroutines, so the cache guards its state with a mutex. The
// mutex only protects the in-memory snapshot and a single catalog load; it never
// spans the per-commit Postgres transaction, so it adds no write serialization.
type repositoryCatalogCache struct {
	mu      sync.Mutex
	loaded  bool
	entries []relationships.CatalogEntry
	repoIDs map[string]struct{}

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
		return catalogSnapshot{Entries: c.entries, RepoIDs: c.repoIDs, CacheHit: true}, nil
	}

	entries, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		return catalogSnapshot{}, err
	}
	c.entries = entries
	c.repoIDs = catalogRepoIDs(entries)
	c.loaded = true
	c.loads.Add(1)

	return catalogSnapshot{Entries: c.entries, RepoIDs: c.repoIDs, CacheHit: false}, nil
}

// invalidateForNewRepositories clears the cache when a committed generation
// introduced a repository identity the cached snapshot did not contain, so the
// next commit reloads a catalog that includes the new repository. It returns
// true when an invalidation occurred. Generations that only touch already-known
// repositories leave the cache intact, which is the common hot-path case.
func (c *repositoryCatalogCache) invalidateForNewRepositories(
	currentGenerationRepoIDs map[string]struct{},
) bool {
	if c == nil || len(currentGenerationRepoIDs) == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		return false
	}
	for repoID := range currentGenerationRepoIDs {
		if _, known := c.repoIDs[repoID]; !known {
			c.loaded = false
			c.entries = nil
			c.repoIDs = nil
			return true
		}
	}

	return false
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
