// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
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

// TestBuildLanguageCypher_JSXBindsJavaScriptSpellings,
// TestBuildLanguageCypher_TSXBindsTypeScriptSpellings, and their
// boundCanonicalLanguage/assertLanguageSpellingsBound helpers moved to
// language_query_cypher_builder_test.go (#6642): they call buildLanguageCypher
// directly -- an unexported language-family free function the mounted route
// does not observably cover -- so they belong in a family-owned white-box
// test file.
