// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"reflect"
	"sort"
	"testing"
)

// sortedAnchors returns a deterministic copy for comparison.
func sortedAnchors(anchors []string) []string {
	out := append([]string(nil), anchors...)
	sort.Strings(out)
	return out
}

func TestCatalogPayloadAnchorsSingleTokenAlias(t *testing.T) {
	t.Parallel()

	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:payments", Aliases: []string{"payments-service"}},
	})
	// "payments-service" tokenizes to one token "payments-service" (the '-' is a
	// token char), so the only anchor is the whole alias lowercased.
	want := []string{"payments-service"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsMultiTokenAliasUsesLongestToken(t *testing.T) {
	t.Parallel()

	// "team payments service" tokenizes to ["team","payments","service"]; the
	// longest token "payments" is the most selective single substring that is
	// guaranteed present whenever the alias matched a candidate.
	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:payments", Aliases: []string{"team payments service"}},
	})
	want := []string{"payments"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsTerraformModuleProviderSuffix(t *testing.T) {
	t.Parallel()

	// A catalog alias like "terraform-modules-aws" is matched by the matcher via
	// the private-registry-provider lookup, where ONLY the "<provider>" path
	// segment ("aws") appears verbatim in the module source payload. The full
	// alias token never appears, so the anchor must include the captured suffix.
	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:tf-aws", Aliases: []string{"terraform-modules-aws"}},
	})
	want := []string{"terraform-modules-aws", "aws"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsTerraformModuleSingularSuffix(t *testing.T) {
	t.Parallel()

	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:tf-gcp", Aliases: []string{"terraform-module-gcp"}},
	})
	want := []string{"terraform-module-gcp", "gcp"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsLowercasesAndDedupes(t *testing.T) {
	t.Parallel()

	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:a", Aliases: []string{"Payments-Service"}},
		{RepoID: "repo:b", Aliases: []string{"payments-service"}},
	})
	want := []string{"payments-service"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsSkipsEmptyAndWhitespace(t *testing.T) {
	t.Parallel()

	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:a", Aliases: []string{"", "   ", "!!!", "valid-repo"}},
	})
	want := []string{"valid-repo"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsKeepsShortTokens(t *testing.T) {
	t.Parallel()

	// Correctness over selectivity: even a 2-char alias must yield an anchor, or
	// the predicate would under-select facts that legitimately match it.
	got := CatalogPayloadAnchors([]CatalogEntry{
		{RepoID: "repo:ab", Aliases: []string{"ab"}},
	})
	want := []string{"ab"}
	if !reflect.DeepEqual(sortedAnchors(got), sortedAnchors(want)) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

func TestCatalogPayloadAnchorsEmptyCatalog(t *testing.T) {
	t.Parallel()

	if got := CatalogPayloadAnchors(nil); got != nil {
		t.Fatalf("anchors = %v, want nil", got)
	}
}

// TestCatalogPayloadAnchorsSupersetOfMatcherTokens proves the superset
// invariant directly: for every alias, every token the matcher tokenizes from
// that alias must be a substring of at least one derived anchor. If an alias
// matched a candidate, all its tokens are substrings of that candidate, so any
// anchor that is itself one of those tokens (or a substring of one) keeps the
// fact selected.
func TestCatalogPayloadAnchorsSupersetOfMatcherTokens(t *testing.T) {
	t.Parallel()

	aliases := []string{
		"payments-service",
		"team payments service",
		"terraform-modules-aws",
		"infra.config.yaml",
		"orders_api",
	}
	for _, alias := range aliases {
		entry := CatalogEntry{RepoID: "repo:x", Aliases: []string{alias}}
		anchors := CatalogPayloadAnchors([]CatalogEntry{entry})
		tokens := catalogMatchTokens(alias)
		if len(tokens) == 0 {
			continue
		}
		// The longest token must be covered by some anchor as a substring.
		longest := tokens[0]
		for _, token := range tokens {
			if len(token) > len(longest) {
				longest = token
			}
		}
		covered := false
		for _, anchor := range anchors {
			if anchor == longest || containsToken(anchor, longest) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("alias %q: longest token %q not covered by anchors %v", alias, longest, anchors)
		}
	}
}

func containsToken(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || stringContains(haystack, needle))
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestCatalogRepoIDValuesReturnsFullRepoIDValues proves the function that drives
// the $2 arm of the deferred-pass self-exclusion SQL predicate (issue #3659). It
// returns each entry's FULL lowercase repo_id value (Aliases[0]), de-duplicated
// and stable-sorted. Full values (not the longest token) are used because
// cross-repo references name a repo by its full URL/path.
func TestCatalogRepoIDValuesReturnsFullRepoIDValues(t *testing.T) {
	t.Parallel()

	catalog := []CatalogEntry{
		// repo_id is Aliases[0]; name is Aliases[1]
		{RepoID: "github.com/org/payments", Aliases: []string{"github.com/org/payments", "payments-service"}},
		{RepoID: "github.com/org/infra", Aliases: []string{"github.com/org/infra", "infra-repo"}},
		// repo with no secondary alias — only repo_id in aliases
		{RepoID: "repo-only", Aliases: []string{"repo-only"}},
	}

	values := CatalogRepoIDValues(catalog)
	if len(values) == 0 {
		t.Fatal("CatalogRepoIDValues is empty, want full repo_id values")
	}

	wantValues := map[string]bool{
		"github.com/org/payments": true,
		"github.com/org/infra":    true,
		"repo-only":               true,
	}
	for _, v := range values {
		if !wantValues[v] {
			t.Errorf("unexpected repo_id value %q", v)
		}
		delete(wantValues, v)
	}
	if len(wantValues) > 0 {
		t.Errorf("missing repo_id values: %v", wantValues)
	}

	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			t.Errorf("values not sorted/de-duped at index %d: %v", i, values)
			break
		}
	}
}

// TestCatalogRepoIDValuesEmptyCatalog returns nil for an empty catalog.
func TestCatalogRepoIDValuesEmptyCatalog(t *testing.T) {
	t.Parallel()

	if values := CatalogRepoIDValues(nil); values != nil {
		t.Fatalf("CatalogRepoIDValues(nil) = %v; want nil", values)
	}
}

// TestCatalogRepoIDValuesExcludesNonRepoIDAliases proves the name/slug aliases
// (Aliases[1:]) do NOT appear in the returned values. Those feed the $1 arm via
// CatalogPayloadAnchors; conflating them would blur the two predicate arms.
func TestCatalogRepoIDValuesExcludesNonRepoIDAliases(t *testing.T) {
	t.Parallel()

	catalog := []CatalogEntry{
		{RepoID: "my-repo-id", Aliases: []string{"my-repo-id", "display-name", "the-slug"}},
	}
	values := CatalogRepoIDValues(catalog)

	for _, v := range values {
		if v == "display-name" || v == "the-slug" {
			t.Errorf("CatalogRepoIDValues contains non-repo_id alias %q", v)
		}
	}
	if len(values) != 1 || values[0] != "my-repo-id" {
		t.Errorf("CatalogRepoIDValues = %v, want [my-repo-id]", values)
	}
}
