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
