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

// catalogFleetWideAnchorToken is the namespace token of every canonical repo_id
// ("repository:r_<8-hex>"). Every Git content and file payload stores its own
// repo_id, so a LIKE term built from this token matches the whole corpus.
const catalogFleetWideAnchorToken = "repository"

// unconditionalPayloadMarkers are the lowercase payload substrings that select a
// fact whatever the catalog's aliases are. They are written for
// lower(payload::text) on a jsonb column, which prints one space after every
// colon. See UnconditionalPayloadMarkers.
var unconditionalPayloadMarkers = []string{
	"kind: application", // Also matches "kind: applicationset".
	`"artifact_type": "argocd"`,
	`"argocd_applications": [{`,
	`"argocd_applicationsets": [{`,
	`"flux_git_repositories": [{`,
}

// UnconditionalPayloadMarkers returns the payload substrings that force facts into
// the scoped fact load regardless of the catalog's aliases, for the two routes
// whose target token need not appear in the fact.
//
// ArgoCD: the ApplicationSet discovery path renders candidate repoURLs by
// substituting template parameters harvested from a different config repository's
// content and from normalized path basenames. Content facts qualify by raw kind or
// artifact_type, and lower() makes that a superset of the case-sensitive
// isArgoCDArtifact check. File facts qualify when an ArgoCD bucket holds a row.
//
// Flux: a GitRepository resolves spec.url by equality with RemoteURL after
// NormalizeRemoteURL, which decodes percent-encoding. "my%2Dapp" resolves to a
// remote ending in "my-app" while the row holds neither that token nor any alias,
// so no anchor derived from the catalog can reach it. Flux evidence comes only from
// this bucket, which makes selecting on the bucket a superset of every spelling.
//
// Each bucket marker follows what the bucket holds, never its key name. Every
// parsed file carries the ArgoCD keys, usually null, so a marker on a key name
// selects every file fact in the fleet. "[{" is the text jsonb prints for an array
// whose first element is an object, the only shape the parser writes. Inside a
// string value the key's quotes are escaped, so the spelling cannot match there.
//
// It returns a copy, so callers may append to it.
func UnconditionalPayloadMarkers() []string {
	return append([]string(nil), unconditionalPayloadMarkers...)
}

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
// The deferred pass's non-repo_id anchors (name and slug tokens plus the
// unconditional markers) come from CatalogNonRepoIDPayloadAnchors — see
// backfillNonRepoIDAnchorTerms.
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
//   - the token catalogAliasAnchorToken picks (a single substring guaranteed
//     present whenever the alias matched, chosen to select few other facts), and
//   - for an alias shaped like "terraform-modules-<provider>" or
//     "terraform-module-<provider>", the captured "<provider>" segment, which is
//     the only payload-visible token for private Terraform registry matches.
//
// Each entry with a RemoteURL also gets the URL's last token. Flux evidence
// resolves spec.url by equality with RemoteURL, not through aliases, and nearly
// every spelling that normalizes to that URL contains its final path segment.
// The exception is percent-encoding inside that segment ("my%2Dapp" for "my-app"),
// which NormalizeRemoteURL decodes. Those rows are safe because
// UnconditionalPayloadMarkers selects every row holding a GitRepository, so this
// anchor is not what keeps them. When the repository's name matches its remote this
// is the anchor the name already gave.
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
			add(catalogAliasAnchorToken(alias, tokens))

			normalized := strings.ToLower(strings.TrimSpace(alias))
			if match := terraformModuleAliasPattern.FindStringSubmatch(normalized); match != nil {
				add(match[1])
			}
		}
		if tokens := catalogMatchTokens(entry.RemoteURL); len(tokens) > 0 {
			add(tokens[len(tokens)-1])
		}
	}

	if len(anchors) == 0 {
		return nil
	}
	sort.Strings(anchors)
	return anchors
}

// CatalogNonRepoIDPayloadAnchors derives the payload anchors of the deferred
// backfill's non-repo_id LIKE parameter: CatalogPayloadAnchors over each entry's
// aliases with the repo_id (Aliases[0]) removed. Every Git content and file payload
// stores its own repo_id, so repo_id-derived tokens would make every fact
// self-match; the deferred query tests repo_ids in a separate parameter after
// stripping the fact's own value (issue #3659). RemoteURL is left out too: only Flux
// resolves through it, and UnconditionalPayloadMarkers selects those rows.
func CatalogNonRepoIDPayloadAnchors(catalog []CatalogEntry) []string {
	stripped := make([]CatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if len(entry.Aliases) <= 1 {
			// Only the repo_id alias; no non-repo_id aliases for this entry.
			continue
		}
		stripped = append(stripped, CatalogEntry{
			RepoID:  entry.RepoID,
			Aliases: entry.Aliases[1:],
		})
	}
	return CatalogPayloadAnchors(stripped)
}

// catalogAliasAnchorToken picks the one token of an alias that anchors it. A
// match needs every token of the alias, so any of them is a safe anchor and the
// choice only decides how many facts the predicate selects.
//
// An alias with a slash is a path, and a path is written owner first ("org/repo",
// "github.com/org/repo"), so its last token names the repository. That token wins
// whatever its length. The owner is the wrong anchor even when it is the longest:
// a fleet shares one owner, and every file row stores a clone path that holds it,
// so an owner term selects most of the corpus.
//
// Any other alias has no such structure, so its longest token wins as the likely
// rarest, and a tie goes to the later token ("repository:r_x").
//
// catalogFleetWideAnchorToken is passed over under both rules, unless the alias
// has no other token: such an alias can only be anchored by that token, and
// over-selecting is safe where under-selecting is not.
func catalogAliasAnchorToken(alias string, tokens []string) string {
	takeLast := strings.Contains(alias, "/")
	anchor := ""
	for _, token := range tokens {
		if token == catalogFleetWideAnchorToken {
			continue
		}
		if takeLast || len(token) >= len(anchor) {
			anchor = token
		}
	}
	if anchor == "" {
		return catalogFleetWideAnchorToken
	}
	return anchor
}
