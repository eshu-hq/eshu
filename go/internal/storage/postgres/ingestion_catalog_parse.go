// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// loadRepositoryCatalog loads the deduplicated repository identity catalog
// (newest row per repository wins via the query ORDER BY) plus each kept
// entry's observed_at, which the shared cache uses as the freshness key so a
// replayed older generation cannot regress a newer cached identity (#5134
// review).
func loadRepositoryCatalog(
	ctx context.Context,
	queryer Queryer,
) ([]relationships.CatalogEntry, map[string]time.Time, error) {
	if queryer == nil {
		return nil, nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listRepositoryCatalogQuery)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	catalog := make([]relationships.CatalogEntry, 0)
	observedAt := make(map[string]time.Time)
	for rows.Next() {
		var rawPayload []byte
		var rowObservedAt time.Time
		if err := rows.Scan(&rawPayload, &rowObservedAt); err != nil {
			return nil, nil, err
		}
		entry, ok := repositoryCatalogEntryFromPayload(rawPayload)
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

func catalogRepoIDs(catalog []relationships.CatalogEntry) map[string]struct{} {
	repoIDs := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		if strings.TrimSpace(entry.RepoID) == "" {
			continue
		}
		repoIDs[entry.RepoID] = struct{}{}
	}
	return repoIDs
}

func repositoryCatalogEntryFromPayload(rawPayload []byte) (relationships.CatalogEntry, bool) {
	if len(rawPayload) == 0 {
		return relationships.CatalogEntry{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return relationships.CatalogEntry{}, false
	}

	return repositoryCatalogEntryFromMap(payload)
}

// repositoryCatalogEntryFromMap derives a repository CatalogEntry (RepoID plus
// matching aliases) from a decoded repository fact payload. It delegates to
// relationships.RepositoryCatalogEntry (#4394 T2) so the streaming commit path,
// the JSON catalog loader, and Ifá's derived-catalog seam
// (go/internal/ifa) all compute a generation's committed repository identity
// identically; otherwise alias-drift detection (issue #3521) would compare
// inconsistently shaped aliases.
func repositoryCatalogEntryFromMap(payload map[string]any) (relationships.CatalogEntry, bool) {
	return relationships.RepositoryCatalogEntry(payload)
}
