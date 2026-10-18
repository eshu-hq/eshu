// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"regexp"
	"strings"
	"testing"
)

// TestFactsQualifierSpellingsStayInSync binds factsPackageIdentRefPattern
// (the query-layer identifier scan) to factsPackageSelectorNames (the
// reducer dispatch scan). Both encode the same #6950/#6951 family-spelling
// migration, and the rule lives otherwise only in comments: without this
// test a future batch that extends one spelling list but not the other
// would silently drop consumer evidence while every other test stays
// green. The alternation is parsed out of the compiled pattern, so the
// test names no spelling twice.
func TestFactsQualifierSpellingsStayInSync(t *testing.T) {
	t.Parallel()

	qualifiers := regexAlternationQualifiers(t, factsPackageIdentRefPattern)

	for _, q := range qualifiers {
		if !factsPackageSelectorNames[q] {
			t.Errorf("qualifier %q is in factsPackageIdentRefPattern but not in factsPackageSelectorNames", q)
		}
	}
	for name := range factsPackageSelectorNames {
		found := false
		for _, q := range qualifiers {
			if q == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("qualifier %q is in factsPackageSelectorNames but not in factsPackageIdentRefPattern", name)
		}
	}
}

// regexAlternationQualifiers returns the branches of the first non-capturing
// group in the pattern source (e.g. ["facts","docs"] for `(?:facts|docs)\.`).
func regexAlternationQualifiers(t *testing.T, pattern *regexp.Regexp) []string {
	t.Helper()

	src := pattern.String()
	start := strings.Index(src, "(?:")
	if start < 0 {
		t.Fatalf("pattern %q has no non-capturing group to parse", src)
	}
	rest := src[start+len("(?:"):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("pattern %q has an unterminated non-capturing group", src)
	}
	alternation := rest[:end]
	if alternation == "" || strings.ContainsAny(alternation, "()\\") {
		t.Fatalf("pattern %q alternation %q is not a plain branch list", src, alternation)
	}
	return strings.Split(alternation, "|")
}
