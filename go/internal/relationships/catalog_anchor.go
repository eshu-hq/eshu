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

// CatalogRepoIDAnchors returns the repo_id-derived anchor tokens and raw values
// needed for the deferred-backfill self-exclusion SQL predicate (issue #3659).
//
// Each catalog entry's first alias is its repo_id (put there by
// repositoryCatalogEntryFromMap via uniqueCatalogAliases). Every Git
// content/file payload carries its own "repo_id" field, so the full
// CatalogPayloadAnchors set — which includes repo_id tokens — causes every
// fact to self-match the LIKE ANY predicate, defeating the scope bounding that
// #3655 claimed. The deferred SQL query must therefore separate:
//
//   - repoIDTokens: the longest catalogMatchTokens token from each entry's
//     first alias (the repo_id). These feed the $2 LIKE ANY arm of the
//     deferred query, which is gated by the $3 self-exclusion predicate.
//   - repoIDValues: the raw lowercase repo_id strings. These feed $3 so the
//     deferred query can express "matched $2 AND payload->>'repo_id' is NOT
//     one of these values", excluding pure self-matches while still loading
//     facts that reference ANOTHER repo's repo_id in their content.
//
// The deferred pass's non-repo_id anchors (name/slug tokens + ArgoCD markers)
// come from CatalogPayloadAnchors applied to a catalog whose Aliases have the
// repo_id first entry stripped — see backfillNonRepoIDAnchorTerms.
//
// Returns nil, nil when the catalog is empty or no entry has a usable first alias.
func CatalogRepoIDAnchors(catalog []CatalogEntry) (repoIDTokens []string, repoIDValues []string) {
	if len(catalog) == 0 {
		return nil, nil
	}

	seenTokens := make(map[string]struct{})
	seenValues := make(map[string]struct{})

	for _, entry := range catalog {
		if len(entry.Aliases) == 0 {
			continue
		}
		// Aliases[0] is the repo_id (guaranteed by repositoryCatalogEntryFromMap).
		repoID := strings.ToLower(strings.TrimSpace(entry.Aliases[0]))
		if repoID == "" {
			continue
		}

		// Collect the raw value for the self-exclusion list.
		if _, ok := seenValues[repoID]; !ok {
			seenValues[repoID] = struct{}{}
			repoIDValues = append(repoIDValues, repoID)
		}

		// Derive the longest token (same logic as CatalogPayloadAnchors) for the
		// LIKE ANY predicate arm.
		tokens := catalogMatchTokens(repoID)
		if len(tokens) == 0 {
			continue
		}
		longest := tokens[0]
		for _, t := range tokens {
			if len(t) > len(longest) {
				longest = t
			}
		}
		if _, ok := seenTokens[longest]; !ok {
			seenTokens[longest] = struct{}{}
			repoIDTokens = append(repoIDTokens, longest)
		}

		// For terraform-module aliases that start with "terraform-modules?-"
		// also emit the provider suffix (mirrors CatalogPayloadAnchors).
		normalized := repoID
		if match := terraformModuleAliasPattern.FindStringSubmatch(normalized); match != nil {
			suffix := match[1]
			if _, ok := seenTokens[suffix]; !ok {
				seenTokens[suffix] = struct{}{}
				repoIDTokens = append(repoIDTokens, suffix)
			}
		}
	}

	if len(repoIDTokens) == 0 && len(repoIDValues) == 0 {
		return nil, nil
	}

	sort.Strings(repoIDTokens)
	sort.Strings(repoIDValues)
	return repoIDTokens, repoIDValues
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
