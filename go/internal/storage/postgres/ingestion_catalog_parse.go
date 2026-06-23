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
// matching aliases) from a decoded repository fact payload. The streaming commit
// path and the JSON catalog loader share this function so a generation's
// committed repository identity is computed identically to the cached catalog
// entry; otherwise alias-drift detection (issue #3521) would compare
// inconsistently shaped aliases.
func repositoryCatalogEntryFromMap(payload map[string]any) (relationships.CatalogEntry, bool) {
	repoID := catalogString(payload, "repo_id", "graph_id", "name")
	if strings.TrimSpace(repoID) == "" {
		return relationships.CatalogEntry{}, false
	}

	aliases := uniqueCatalogAliases(
		repoID,
		catalogString(payload, "name", "repo_name"),
		catalogString(payload, "repo_slug"),
	)

	return relationships.CatalogEntry{
		RepoID:  repoID,
		Aliases: aliases,
	}, true
}

func catalogString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func uniqueCatalogAliases(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	aliases := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		aliases = append(aliases, value)
	}
	return aliases
}
