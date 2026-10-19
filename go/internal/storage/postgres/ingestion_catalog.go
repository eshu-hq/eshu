// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// repositoryCatalog returns the shared repository identity catalog, loading it
// once through the supplied queryer when the cache is cold. The caller MUST pass
// the open ingestion transaction: a cold load must reuse the transaction's
// connection rather than acquiring a second pool connection while the tx is
// open, which would deadlock under a saturated or single-connection pool
// (ESHU_POSTGRES_MAX_OPEN_CONNS=1). Reading on the transaction is also correct:
// the catalog reflects committed global repository facts plus this
// transaction's own writes, and this generation's repository facts are not yet
// written at load time and are not evidence targets for themselves.
func (s IngestionStore) repositoryCatalog(ctx context.Context, queryer db.Queryer) (catalogSnapshot, error) {
	return s.catalogCache.get(ctx, queryer)
}

// mergeCatalogForChangedRepositories merges a committed generation's
// repository identities into the shared catalog cache when the generation
// introduced a repository the cache had not seen or changed a known
// repository's identity aliases. It returns true when the cache changed.
// See repositoryCatalogCache.mergeChangedRepositories for the #5129
// merge-vs-evict rationale and equivalence contract.
func (s IngestionStore) mergeCatalogForChangedRepositories(
	currentGenerationRepos map[string]relationships.CatalogEntry,
	currentGenerationObservedAt map[string]time.Time,
) bool {
	return s.catalogCache.mergeChangedRepositories(currentGenerationRepos, currentGenerationObservedAt)
}

// catalogEntryIDSet projects a repo-id-to-CatalogEntry map down to the set of
// repository ids. The new-repository relationship backfill keys only on ids,
// while cache invalidation needs the full identity.
func catalogEntryIDSet(entries map[string]relationships.CatalogEntry) map[string]struct{} {
	if len(entries) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(entries))
	for repoID := range entries {
		ids[repoID] = struct{}{}
	}
	return ids
}

// catalogLoadCount reports how many fresh repository catalog loads the shared
// cache has performed. It feeds the commit-stage log so operators can confirm
// the hot path is not reloading the catalog per commit.
func (s IngestionStore) catalogLoadCount() int64 {
	return s.catalogCache.loadCount()
}
