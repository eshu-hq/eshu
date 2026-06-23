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
