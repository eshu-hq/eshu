// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "strings"

// RepositoryCatalogEntry derives a repository CatalogEntry (RepoID plus
// matching aliases) from a decoded repository fact payload. It is the single
// source of truth for that derivation: the Postgres streaming commit path
// (go/internal/storage/postgres) and Ifá's derived-catalog seam
// (go/internal/ifa, #4394) both call it so a generation's committed repository
// identity is computed identically to any offline catalog derived from the same
// facts — otherwise alias-drift detection (issue #3521) would compare
// inconsistently shaped aliases.
//
// Aliases includes RepoID itself as its first entry (matching candidates are
// matched by alias, and RepoID is always a valid match target), followed by
// the payload's name/repo_name and repo_slug when present and distinct.
//
// The ok result is false when payload carries none of repo_id, graph_id, or
// name — RepoID would otherwise be blank, which is never a valid catalog entry.
func RepositoryCatalogEntry(payload map[string]any) (CatalogEntry, bool) {
	repoID := catalogPayloadString(payload, "repo_id", "graph_id", "name")
	if strings.TrimSpace(repoID) == "" {
		return CatalogEntry{}, false
	}

	aliases := uniqueRepositoryCatalogAliases(
		repoID,
		catalogPayloadString(payload, "name", "repo_name"),
		catalogPayloadString(payload, "repo_slug"),
	)

	return CatalogEntry{
		RepoID:  repoID,
		Aliases: aliases,
	}, true
}

func catalogPayloadString(payload map[string]any, keys ...string) string {
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

// uniqueRepositoryCatalogAliases returns the non-blank, order-preserving,
// de-duplicated set of alias candidates.
func uniqueRepositoryCatalogAliases(values ...string) []string {
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
