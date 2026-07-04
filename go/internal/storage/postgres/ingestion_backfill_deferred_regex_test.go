// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "testing"

func TestBuildDeferredRepoIDRegexExcludesOwnValue(t *testing.T) {
	regex, ok := buildDeferredRepoIDRegex([]string{"repo-a", "repo-b", "repo-c"}, "repo-b")
	if !ok {
		t.Fatal("expected ok=true when the exclusion leaves values behind")
	}
	if regex != "(?:repo-a|repo-c)" {
		t.Fatalf("expected own value excluded and remaining values joined, got %q", regex)
	}
}

func TestBuildDeferredRepoIDRegexEscapesRegexMetacharacters(t *testing.T) {
	regex, ok := buildDeferredRepoIDRegex([]string{`repo.name+v1(x)[y]{2}^$|\`}, "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := `(?:repo\.name\+v1\(x\)\[y\]\{2\}\^\$\|\\)`
	if regex != want {
		t.Fatalf("expected every ARE metacharacter escaped\n got: %q\nwant: %q", regex, want)
	}
}

func TestBuildDeferredRepoIDRegexDeduplicatesCaseInsensitively(t *testing.T) {
	regex, ok := buildDeferredRepoIDRegex([]string{"Repo-A", "repo-a", "REPO-A"}, "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if regex != "(?:repo-a)" {
		t.Fatalf("expected case-insensitive dedupe to a single lowercase entry, got %q", regex)
	}
}

func TestBuildDeferredRepoIDRegexEmptyCatalogNotOK(t *testing.T) {
	if _, ok := buildDeferredRepoIDRegex(nil, "repo-a"); ok {
		t.Fatal("expected ok=false for an empty catalog")
	}
}

func TestBuildDeferredRepoIDRegexAllValuesExcludedNotOK(t *testing.T) {
	// Single-entry catalog where the only value IS the own value: excluding it
	// leaves nothing, so the caller must skip the fast arm entirely rather than
	// build the empty alternation "(?:)", which matches every string in
	// Postgres ARE (verified directly: `SELECT 'x' ~ '(?:)'` is true).
	if _, ok := buildDeferredRepoIDRegex([]string{"repo-a"}, "repo-a"); ok {
		t.Fatal("expected ok=false when excluding own leaves zero values (would build a match-everything regex)")
	}
}

func TestBuildDeferredRepoIDRegexBlankAndDuplicateValuesIgnored(t *testing.T) {
	regex, ok := buildDeferredRepoIDRegex([]string{"", "  ", "repo-a", "repo-a"}, "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if regex != "(?:repo-a)" {
		t.Fatalf("expected blank/duplicate values dropped, got %q", regex)
	}
}

func TestDeferredScopedFactOwnRepoIDFromScope(t *testing.T) {
	cases := []struct {
		name    string
		scopeID string
		want    string
	}{
		{"git repository scope lowercases repo id", "git-repository-scope:GitHub.com/Org/App", "github.com/org/app"},
		{"git repository scope trims whitespace", "git-repository-scope:  repo-a  ", "repo-a"},
		{"gcp relationship scope has no single own repo id", "gcp:project:hoist:relationship:global", ""},
		{"empty scope id", "", ""},
		{"unrelated scope prefix", "vault-cluster-scope:cluster-a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deferredScopedFactOwnRepoIDFromScope(tc.scopeID)
			if got != tc.want {
				t.Fatalf("deferredScopedFactOwnRepoIDFromScope(%q) = %q, want %q", tc.scopeID, got, tc.want)
			}
		})
	}
}
