// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"encoding/json"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// listRepositoryCatalogQuery loads every repository fact payload, newest
// first, for the catalog loader below.
const listRepositoryCatalogQuery = `
SELECT payload, observed_at
FROM fact_records
WHERE fact_kind = 'repository'
ORDER BY observed_at DESC, fact_id DESC
`

// catalogLoadStats counts what one LoadRepositoryCatalog call did, so the
// deferred pass's catalog_load block can report rows scanned and decode time.
// The loader returns only the kept entries, so the scanned row count and the
// JSON decode time travel through the context instead of the CatalogLoader
// signature.
type catalogLoadStats struct {
	rows   int64
	decode time.Duration
}

type catalogLoadStatsKey struct{}

// withCatalogLoadStats returns ctx carrying stats for LoadRepositoryCatalog to
// fill. Without it the loader does no extra work.
func withCatalogLoadStats(ctx context.Context, stats *catalogLoadStats) context.Context {
	return context.WithValue(ctx, catalogLoadStatsKey{}, stats)
}

// LoadRepositoryCatalog loads the deduplicated repository identity catalog
// (newest row per repository wins via the query ORDER BY) plus each kept
// entry's observed_at, which the shared cache uses as the freshness key so a
// replayed older generation cannot regress a newer cached identity (#5134
// review).
func LoadRepositoryCatalog(
	ctx context.Context,
	queryer db.Queryer,
) ([]relationships.CatalogEntry, map[string]time.Time, error) {
	if queryer == nil {
		return nil, nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listRepositoryCatalogQuery)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	stats, _ := ctx.Value(catalogLoadStatsKey{}).(*catalogLoadStats)
	seen := make(map[string]struct{})
	catalog := make([]relationships.CatalogEntry, 0)
	observedAt := make(map[string]time.Time)
	for rows.Next() {
		var rawPayload []byte
		var rowObservedAt time.Time
		if err := rows.Scan(&rawPayload, &rowObservedAt); err != nil {
			return nil, nil, err
		}
		var decodeStart time.Time
		if stats != nil {
			stats.rows++
			decodeStart = time.Now()
		}
		entry, ok := repositoryCatalogEntryFromPayload(rawPayload)
		if stats != nil {
			stats.decode += time.Since(decodeStart)
		}
		if !ok {
			continue
		}
		if _, exists := seen[entry.RepoID]; exists {
			continue
		}
		seen[entry.RepoID] = struct{}{}
		catalog = append(catalog, entry)
		observedAt[entry.RepoID] = rowObservedAt
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return catalog, observedAt, nil
}

func repositoryCatalogEntryFromPayload(rawPayload []byte) (relationships.CatalogEntry, bool) {
	if len(rawPayload) == 0 {
		return relationships.CatalogEntry{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return relationships.CatalogEntry{}, false
	}

	return RepositoryCatalogEntryFromMap(payload)
}

// RepositoryCatalogEntryFromMap derives a repository CatalogEntry (RepoID plus
// matching aliases) from a decoded repository fact payload. It delegates to
// relationships.RepositoryCatalogEntry (#4394 T2) so the streaming commit path,
// the JSON catalog loader, and Ifá's derived-catalog seam
// (go/internal/ifa) all compute a generation's committed repository identity
// identically; otherwise alias-drift detection (issue #3521) would compare
// inconsistently shaped aliases.
func RepositoryCatalogEntryFromMap(payload map[string]any) (relationships.CatalogEntry, bool) {
	return relationships.RepositoryCatalogEntry(payload)
}
