// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

// languageAliases maps a language spelling variant to its canonical name.
// It is deliberately unexported: callers go through CanonicalLanguage or
// NormalizedLanguageVariants rather than reading the map, so the contract
// surface stays a pair of pure functions and no caller can mutate shared
// state at runtime.
//
// This moved here from root package query's language_registry.go (#6060) so
// a handler-family subpackage can normalize a language name identically to
// root, rather than a package-local copy that could silently drift as
// languages are added -- this is actively maintained taxonomy, not a pure
// stateless helper. Root has no alias for this map: its callers were updated to
// name the leaf package directly.
var languageAliases = map[string]string{
	"jsx": "javascript",
	"tsx": "typescript",
}

// CanonicalLanguage lowercases, trims, and resolves language through
// languageAliases to its canonical spelling.
func CanonicalLanguage(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if canonical, ok := languageAliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// CoverageLanguageMaps flattens a repository's language coverage counts into
// the map shape the coverage response wire contract expects.
//
// This moved here from root package query's repository_coverage.go (#6060)
// alongside the other language-taxonomy helpers.
func CoverageLanguageMaps(languages []RepositoryLanguageCount) []map[string]any {
	if len(languages) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(languages))
	for _, language := range languages {
		result = append(result, map[string]any{
			"language":   language.Language,
			"file_count": language.FileCount,
		})
	}
	return result
}

// NormalizedLanguageVariants returns every spelling variant a caller should
// match against for language, so a query for "javascript" also matches
// "jsx"-labeled content and "typescript" also matches "tsx".
func NormalizedLanguageVariants(language string) []string {
	switch CanonicalLanguage(language) {
	case "javascript":
		return []string{"javascript", "jsx"}
	case "typescript":
		return []string{"typescript", "tsx"}
	default:
		return []string{CanonicalLanguage(language)}
	}
}
