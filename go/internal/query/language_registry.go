// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// supportedLanguages lists every language name accepted by language-query.
var supportedLanguages = map[string]bool{
	"c": true, "cpp": true, "csharp": true, "dart": true,
	"elixir": true, "go": true, "haskell": true, "java": true,
	"javascript": true, "jsx": true, "hcl": true, "kotlin": true,
	"perl": true, "php": true, "python": true, "ruby": true,
	"rust": true, "scala": true, "sql": true, "swift": true,
	"typescript": true, "tsx": true,
}

func canonicalLanguage(language string) string {
	return querycontract.CanonicalLanguage(language)
}

func normalizedLanguageVariants(language string) []string {
	return querycontract.NormalizedLanguageVariants(language)
}

// graphLanguageSpellings returns every value the graph's `language` property
// may hold for one query language, for a `language IN $languages` predicate.
//
// The projector writes the parser's own spelling, which is the canonical name
// for most languages but `tsx` for the TSX parser, `jsx`-era rows for the
// JavaScript one, and `c_sharp` for C#; normalizedLanguageVariants owns that
// list. Each spelling is also emitted Title-cased, because Python-era
// projections wrote `Python` and `Go`, and rows of that vintage can still be
// in a retained store. The list is deduplicated and keeps the canonical
// spelling first.
func graphLanguageSpellings(language string) []string {
	variants := normalizedLanguageVariants(language)
	spellings := make([]string, 0, 2*len(variants))
	seen := make(map[string]struct{}, 2*len(variants))
	for _, variant := range variants {
		for _, spelling := range []string{variant, strings.Title(variant)} { //nolint:staticcheck
			if _, dup := seen[spelling]; dup {
				continue
			}
			seen[spelling] = struct{}{}
			spellings = append(spellings, spelling)
		}
	}
	return spellings
}
