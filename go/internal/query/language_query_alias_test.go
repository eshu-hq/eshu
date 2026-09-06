// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"
)

func TestSupportedLanguages_ExplicitJSXAndTSX(t *testing.T) {
	langs := SupportedLanguages()
	langSet := make(map[string]bool, len(langs))
	for _, lang := range langs {
		langSet[lang] = true
	}

	for _, want := range []string{"jsx", "tsx"} {
		if !langSet[want] {
			t.Fatalf("SupportedLanguages() missing %q in %#v", want, langs)
		}
	}
}

// TestBuildLanguageCypher_JSXBindsJavaScriptSpellings: a jsx request is a
// javascript request whose bound spelling list still reaches jsx-stamped
// rows. The predicate is `language IN $languages`; there is no extension
// fallback to carry the alias (#6546).
func TestBuildLanguageCypher_JSXBindsJavaScriptSpellings(t *testing.T) {
	cypher, params := buildLanguageCypher("jsx", "File", "Button", "", 5)

	if got, want := boundCanonicalLanguage(t, params), "javascript"; got != want {
		t.Fatalf("bound canonical language = %#v, want %#v", got, want)
	}
	if !searchString(cypher, "f.language IN $languages") {
		t.Fatalf("buildLanguageCypher(\"jsx\") missing the spelling-list predicate in %q", cypher)
	}
	assertLanguageSpellingsBound(t, params, "javascript", "jsx")
}

func TestBuildLanguageCypher_TSXBindsTypeScriptSpellings(t *testing.T) {
	cypher, params := buildLanguageCypher("tsx", "File", "Component", "", 5)

	if got, want := boundCanonicalLanguage(t, params), "typescript"; got != want {
		t.Fatalf("bound canonical language = %#v, want %#v", got, want)
	}
	if !searchString(cypher, "f.language IN $languages") {
		t.Fatalf("buildLanguageCypher(\"tsx\") missing the spelling-list predicate in %q", cypher)
	}
	assertLanguageSpellingsBound(t, params, "typescript", "tsx")
}

// boundCanonicalLanguage returns the first entry of the bound $languages list,
// which graphLanguageSpellings documents as the canonical name.
func boundCanonicalLanguage(t *testing.T, params map[string]any) string {
	t.Helper()
	bound, ok := params["languages"].([]string)
	if !ok || len(bound) == 0 {
		t.Fatalf("params[languages] = %#v, want a non-empty []string", params["languages"])
	}
	return bound[0]
}

// assertLanguageSpellingsBound checks that every named spelling is in the
// bound $languages list, and that nothing that looks like a file extension is.
func assertLanguageSpellingsBound(t *testing.T, params map[string]any, want ...string) {
	t.Helper()
	bound, ok := params["languages"].([]string)
	if !ok {
		t.Fatalf("params[languages] = %#v, want a []string", params["languages"])
	}
	for _, spelling := range want {
		if !slices.Contains(bound, spelling) {
			t.Fatalf("params[languages] = %v, missing %q", bound, spelling)
		}
	}
	for _, spelling := range bound {
		if strings.HasPrefix(spelling, ".") {
			t.Fatalf("params[languages] = %v carries a file extension; the graph filter matches the language property only", bound)
		}
	}
}
