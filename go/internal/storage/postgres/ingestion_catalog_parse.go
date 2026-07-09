// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func loadRepositoryCatalog(ctx context.Context, queryer Queryer) ([]relationships.CatalogEntry, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listRepositoryCatalogQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	catalog := make([]relationships.CatalogEntry, 0)
	for rows.Next() {
		var rawPayload []byte
		if err := rows.Scan(&rawPayload); err != nil {
			return nil, err
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
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return catalog, nil
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
