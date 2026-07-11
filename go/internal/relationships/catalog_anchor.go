// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"regexp"
	"sort"
	"strings"
)

// terraformModuleAliasPattern captures the provider suffix of a catalog alias
// that names a private Terraform module registry repository
// ("terraform-modules-<provider>" or "terraform-module-<provider>"). The
// catalog matcher resolves such aliases through privateTerraformRegistryProvider:
// for a module source host.tld/namespace/<name>/<provider> only the <provider>
// path segment appears verbatim in the fact payload, never the full alias token.
// A payload predicate keyed on the full alias would therefore under-select these
// facts, so the captured provider suffix must be added as a separate anchor.
var terraformModuleAliasPattern = regexp.MustCompile(`^terraform-modules?-(.+)$`)

// CatalogRepoIDValues returns each catalog entry's full lowercase repo_id value,
// de-duplicated and stable-sorted. These feed the $2 LIKE-ANY arm of the
// deferred-backfill self-exclusion query (issue #3659).
//
// Each catalog entry's first alias is its repo_id (put there by
// repositoryCatalogEntryFromMap via uniqueCatalogAliases). Every Git
// content/file payload carries its own "repo_id" field, so an anchor set that
// includes repo_id tokens causes every fact to self-match the LIKE ANY
// predicate, defeating the scope bounding that #3655 claimed.
//
// The deferred query tests these values against the payload text AFTER stripping
// the fact's OWN repo_id value, so a fact matches only when it references
// ANOTHER repo's repo_id verbatim (the legitimate cross-repo reference). The
// FULL repo_id value is used — not the longest catalogMatchTokens token —
// because repo_ids are referenced across repos by their full URL/path (e.g.
// github.com/org/repo in go.mod), so the full value is the substring that
// actually appears, a shared prefix token (github.com) would over-select the
// fleet, and stripping a token rather than the full value could corrupt
// overlapping tokens.
//
// The deferred pass's non-repo_id anchors (name/slug tokens + ArgoCD markers)
// come from CatalogPayloadAnchors applied to a catalog whose Aliases have the
// repo_id first entry stripped — see backfillNonRepoIDAnchorTerms.
//
// Returns nil when the catalog is empty or no entry has a usable repo_id alias.
func CatalogRepoIDValues(catalog []CatalogEntry) []string {
	if len(catalog) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(catalog))
	values := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		if len(entry.Aliases) == 0 {
			continue
		}
		// Aliases[0] is the repo_id (guaranteed by repositoryCatalogEntryFromMap).
		repoID := strings.ToLower(strings.TrimSpace(entry.Aliases[0]))
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		values = append(values, repoID)
	}

	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	return values
}

// CatalogReferenceKey returns the boundary-aware token key used to compare a
// catalog repo_id against precomputed relationship reference streams. It uses
// the same tokenization as catalogMatcher, so prefix collisions such as
// "github.com/org/app" versus "github.com/org/app-config" do not collapse.
func CatalogReferenceKey(value string) string {
	tokens := catalogMatchTokens(value)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, "|")
}

// CatalogReferenceTokenStream returns a delimiter-wrapped token stream for a
// relationship candidate payload. Callers can test for
// "|" + CatalogReferenceKey(repo_id) + "|" to get the same whole-token
// containment semantics catalogMatcher applies after SQL has selected a fact.
func CatalogReferenceTokenStream(value string) string {
	key := CatalogReferenceKey(value)
	if key == "" {
		return ""
	}
	return "|" + key + "|"
}

// CatalogPayloadAnchors derives the set of lowercase payload-substring anchors
// that a content-scoped SQL fact load must test so its result is a provable
// superset of the facts the in-memory catalogMatcher would match against the
// same catalog.
//
// The matcher tokenizes a candidate string with catalogMatchTokens and accepts
// an alias only when the alias's tokens appear as a consecutive token
// subsequence of the candidate. Every token of a matched alias is therefore a
// substring of the lowercased candidate string, and (because content/file/gcp
// fact payloads store candidate strings verbatim under payload jsonb) a substring
// of lower(payload::text). For each alias this function emits:
//
//   - the longest catalogMatchTokens token (the most selective single substring
//     that is guaranteed present whenever the alias matched), and
//   - for an alias shaped like "terraform-modules-<provider>" or
//     "terraform-module-<provider>", the captured "<provider>" segment, which is
//     the only payload-visible token for private Terraform registry matches.
//
// Anchors are lowercased, trimmed, de-duplicated, and stable-sorted for
// deterministic SQL parameters. Short tokens are retained on purpose: dropping
// them for selectivity would under-select facts that legitimately match a short
// alias, which is a correlation-truth bug. Over-selection is safe; under-selection
// is not.
func CatalogPayloadAnchors(catalog []CatalogEntry) []string {
	if len(catalog) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	anchors := make([]string, 0, len(catalog))
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		anchors = append(anchors, value)
	}

	for _, entry := range catalog {
		for _, alias := range entry.Aliases {
			tokens := catalogMatchTokens(alias)
			if len(tokens) == 0 {
				continue
			}
			longest := tokens[0]
			for _, token := range tokens {
				if len(token) > len(longest) {
					longest = token
				}
			}
			add(longest)

			normalized := strings.ToLower(strings.TrimSpace(alias))
			if match := terraformModuleAliasPattern.FindStringSubmatch(normalized); match != nil {
				add(match[1])
			}
		}
	}

	if len(anchors) == 0 {
		return nil
	}
	sort.Strings(anchors)
	return anchors
}
