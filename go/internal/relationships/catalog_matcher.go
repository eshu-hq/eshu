// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"sort"
	"strings"
	"unicode"
)

type catalogMatcher struct {
	entries      []CatalogEntry
	singleToken  map[string][]catalogAlias
	multiByFirst map[string][]catalogAlias
}

type catalogAlias struct {
	entryIndex int
	aliasIndex int
	alias      string
	normalized string
	tokens     []string
}

type catalogMatch struct {
	entry CatalogEntry
	alias string
}

func newCatalogMatcher(catalog []CatalogEntry) *catalogMatcher {
	matcher := &catalogMatcher{
		entries:      catalog,
		singleToken:  make(map[string][]catalogAlias),
		multiByFirst: make(map[string][]catalogAlias),
	}
	for entryIndex, entry := range catalog {
		for aliasIndex, alias := range entry.Aliases {
			normalized := strings.ToLower(strings.TrimSpace(alias))
			tokens := catalogMatchTokens(normalized)
			if len(tokens) == 0 {
				continue
			}
			item := catalogAlias{
				entryIndex: entryIndex,
				aliasIndex: aliasIndex,
				alias:      alias,
				normalized: normalized,
				tokens:     tokens,
			}
			if len(tokens) == 1 {
				matcher.singleToken[tokens[0]] = append(matcher.singleToken[tokens[0]], item)
				continue
			}
			matcher.multiByFirst[tokens[0]] = append(matcher.multiByFirst[tokens[0]], item)
		}
	}
	return matcher
}

func (matcher *catalogMatcher) match(candidate, sourceRepoID string) []catalogMatch {
	if matcher == nil || len(matcher.entries) == 0 {
		return nil
	}
	tokens := catalogMatchTokens(candidate)
	if len(tokens) == 0 {
		return nil
	}

	best := make(map[int]catalogAlias)
	matchedIndexes := make([]int, 0, 4)
	consider := func(alias catalogAlias) {
		entry := matcher.entries[alias.entryIndex]
		if entry.RepoID == sourceRepoID {
			return
		}
		current, ok := best[alias.entryIndex]
		if !ok {
			best[alias.entryIndex] = alias
			matchedIndexes = append(matchedIndexes, alias.entryIndex)
			return
		}
		if catalogAliasIsBetter(alias, current) {
			best[alias.entryIndex] = alias
		}
	}

	for index, token := range tokens {
		for _, alias := range matcher.singleToken[token] {
			consider(alias)
		}
		for _, alias := range matcher.multiByFirst[token] {
			if catalogAliasTokensMatch(tokens, index, alias.tokens) {
				consider(alias)
			}
		}
	}
	if provider, ok := privateTerraformRegistryProvider(candidate); ok {
		for _, alias := range matcher.singleToken["terraform-modules-"+provider] {
			consider(alias)
		}
		for _, alias := range matcher.singleToken["terraform-module-"+provider] {
			consider(alias)
		}
	}

	sort.Ints(matchedIndexes)
	matches := make([]catalogMatch, 0, len(matchedIndexes))
	for _, entryIndex := range matchedIndexes {
		matches = append(matches, catalogMatch{
			entry: matcher.entries[entryIndex],
			alias: best[entryIndex].alias,
		})
	}
	return matches
}

func catalogAliasIsBetter(candidate, current catalogAlias) bool {
	if len(candidate.tokens) != len(current.tokens) {
		return len(candidate.tokens) > len(current.tokens)
	}
	if len(candidate.normalized) != len(current.normalized) {
		return len(candidate.normalized) > len(current.normalized)
	}
	return candidate.aliasIndex < current.aliasIndex
}

func catalogAliasTokensMatch(candidate []string, offset int, alias []string) bool {
	if offset+len(alias) > len(candidate) {
		return false
	}
	for index, token := range alias {
		if candidate[offset+index] != token {
			return false
		}
	}
	return true
}

func catalogMatchTokens(value string) []string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	tokens := make([]string, 0, 4)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		token := normalizeCatalogMatchToken(builder.String())
		builder.Reset()
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	for _, char := range normalized {
		if isCatalogTokenChar(char) {
			builder.WriteRune(char)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func normalizeCatalogMatchToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimSuffix(token, ".git")
	token = trimCatalogMatchFileExtension(token)
	return strings.TrimSpace(token)
}

func trimCatalogMatchFileExtension(token string) string {
	for _, extension := range []string{".yaml", ".yml", ".json", ".tf", ".tfvars", ".hcl"} {
		if strings.HasSuffix(token, extension) {
			return strings.TrimSuffix(token, extension)
		}
	}
	return token
}

func isCatalogTokenChar(char rune) bool {
	return unicode.IsLetter(char) ||
		unicode.IsDigit(char) ||
		char == '.' ||
		char == '_' ||
		char == '-'
}

// matchCatalog matches a candidate string against catalog entries and returns
// evidence facts for each match.
func matchCatalog(
	sourceRepoID, candidate, filePath string,
	evidenceKind EvidenceKind,
	relType RelationshipType,
	confidence float64,
	rationale, extractor string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	extraDetails map[string]any,
) []EvidenceFact {
	var evidence []EvidenceFact

	for _, match := range matcher.match(candidate, sourceRepoID) {
		entry := match.entry
		key := evidenceKey{
			EvidenceKind:   evidenceKind,
			SourceRepoID:   sourceRepoID,
			TargetRepoID:   entry.RepoID,
			SourceEntityID: "",
			TargetEntityID: "",
			Path:           filePath,
			MatchedValue:   candidate,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		details := map[string]any{
			"path":          filePath,
			"matched_alias": match.alias,
			"matched_value": candidate,
			"extractor":     extractor,
		}
		for key, value := range extraDetails {
			details[key] = value
		}
		evidence = append(evidence, EvidenceFact{
			EvidenceKind:     evidenceKind,
			RelationshipType: relType,
			SourceRepoID:     sourceRepoID,
			TargetRepoID:     entry.RepoID,
			Confidence:       confidence,
			Rationale:        rationale,
			Details:          details,
		})
	}

	return evidence
}

// remoteURLMatches returns every catalog entry whose RemoteURL is BYTE-EQUAL
// to normalizedURL. Unlike match, this is a strict identity comparison, not a
// fuzzy token match: it is the resolution primitive
// discoverStructuredFluxEvidence uses so a Flux GitRepository's spec.url only
// ever links to a repository it names exactly, never one it merely resembles
// (issue #5483 C2). An empty normalizedURL or an entry with no RemoteURL
// never matches.
func (matcher *catalogMatcher) remoteURLMatches(normalizedURL string) []CatalogEntry {
	if matcher == nil || normalizedURL == "" {
		return nil
	}
	var matches []CatalogEntry
	for _, entry := range matcher.entries {
		if entry.RemoteURL == "" {
			continue
		}
		if entry.RemoteURL == normalizedURL {
			matches = append(matches, entry)
		}
	}
	return matches
}

// matchesEntry checks if a candidate string matches any alias of a catalog entry.
// Returns the matched alias or empty string.
func matchesEntry(candidate string, entry CatalogEntry) string {
	matcher := newCatalogMatcher([]CatalogEntry{entry})
	matches := matcher.match(candidate, "")
	if len(matches) == 0 {
		return ""
	}
	return matches[0].alias
}
