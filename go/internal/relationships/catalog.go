// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// RepositoryCatalogEntry derives a repository CatalogEntry (RepoID, matching
// aliases, and normalized RemoteURL) from a decoded repository fact payload.
// It is the single source of truth for that derivation: the Postgres
// streaming commit path (go/internal/storage/postgres) and Ifá's
// derived-catalog seam (go/internal/ifa, #4394) both call it so a
// generation's committed repository identity is computed identically to any
// offline catalog derived from the same facts — otherwise alias-drift
// detection (issue #3521) would compare inconsistently shaped aliases.
//
// Aliases includes RepoID itself as its first entry (matching candidates are
// matched by alias, and RepoID is always a valid match target), followed by
// the payload's name/repo_name and repo_slug when present and distinct.
//
// RemoteURL is the repositoryidentity.NormalizeRemoteURL-normalized
// payload["remote_url"] (empty when absent). It feeds ONLY
// discoverStructuredFluxEvidence's strict cross-repo URL resolution (issue
// #5483 C2) and is never folded into Aliases.
//
// The ok result is false when payload carries none of repo_id, graph_id, or
// name — RepoID would otherwise be blank, which is never a valid catalog entry.
//
// The derivation reads only the keys RepositoryCatalogPayloadKeys declares.
// It projects payload onto those keys first, so a read of any other key sees
// nothing, and a SQL projection of the same keys (the backfill catalog loader,
// #8082) derives the identical entry from a much smaller payload.
func RepositoryCatalogEntry(payload map[string]any) (CatalogEntry, bool) {
	payload = projectRepositoryCatalogPayload(payload)
	repoID := catalogPayloadString(payload, "repo_id", "graph_id", "name")
	if strings.TrimSpace(repoID) == "" {
		return CatalogEntry{}, false
	}

	aliases := uniqueRepositoryCatalogAliases(
		repoID,
		catalogPayloadString(payload, "name", "repo_name"),
		catalogPayloadString(payload, "repo_slug"),
	)

	// RemoteURL is deliberately NOT folded into aliases (see CatalogEntry's
	// doc comment): it feeds only discoverStructuredFluxEvidence's strict
	// equality resolution, never the fuzzy alias/token matcher.
	remoteURL := repositoryidentity.NormalizeRemoteURL(catalogPayloadString(payload, "remote_url"))

	return CatalogEntry{
		RepoID:    repoID,
		Aliases:   aliases,
		RemoteURL: remoteURL,
	}, true
}

// repositoryCatalogPayloadKeys lists every repository-fact payload key
// RepositoryCatalogEntry reads, in the precedence order of its reads: the
// RepoID keys (repo_id, graph_id, name), then the alias keys (name, repo_name,
// repo_slug, where name is already listed), then remote_url.
// TestRepositoryCatalogEntryReadsExactlyTheDeclaredKeys keeps the literals
// passed to catalogPayloadString equal to this list.
var repositoryCatalogPayloadKeys = [...]string{
	"repo_id", "graph_id", "name", "repo_name", "repo_slug", "remote_url",
}

// RepositoryCatalogPayloadKeys returns the repository-fact payload keys that
// RepositoryCatalogEntry reads. It is the single list the Postgres catalog
// loader (go/internal/storage/postgres/backfill) projects in SQL so the
// database ships only these keys instead of whole repository payloads (#8082).
// The caller owns the returned slice.
func RepositoryCatalogPayloadKeys() []string {
	return append([]string(nil), repositoryCatalogPayloadKeys[:]...)
}

// projectRepositoryCatalogPayload copies the declared keys of payload that are
// present, so RepositoryCatalogEntry cannot depend on any other key.
func projectRepositoryCatalogPayload(payload map[string]any) map[string]any {
	projected := make(map[string]any, len(repositoryCatalogPayloadKeys))
	for _, key := range repositoryCatalogPayloadKeys {
		if value, ok := payload[key]; ok {
			projected[key] = value
		}
	}
	return projected
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
