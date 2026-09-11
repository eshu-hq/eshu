// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This file is a family-owned white-box test file (#6642): every test here
// calls buildLanguageCypher, joinKeys, or sortStrings directly -- unexported
// language-family free functions whose output (Cypher text, parameter maps,
// sorted/joined strings) no HTTP response body carries, so the mounted route
// does not observably cover these assertions. They stay grouped here rather
// than driven through the route, and travel with the family on its later
// move to its own package. No Cypher shape changes; text is unchanged from
// its prior home.

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

func TestBuildLanguageCypher_Function(t *testing.T) {
	cypher, params := buildLanguageCypher("python", "Function", "my_func", "repo:123", 10)

	if cypher == "" {
		t.Fatal("expected non-empty cypher")
	}
	// Must contain the Function label.
	if !querytestutil.SearchString(cypher, "Function") {
		t.Error("cypher should contain Function label")
	}
	// The canonical language leads the bound spelling list; no bare
	// $language parameter is bound because no builder references one.
	if got := querytestutil.BoundCanonicalLanguage(t, params); got != "python" {
		t.Errorf("bound canonical language = %v, want python", got)
	}
	if _, ok := params["language"]; ok {
		t.Error("language param should not be bound; the builders reference $languages only")
	}
	if params["repo_id"] != "repo:123" {
		t.Errorf("repo_id param = %v, want repo:123", params["repo_id"])
	}
	if params["query"] != "my_func" {
		t.Errorf("query param = %v, want my_func", params["query"])
	}
	if params["limit"] != 10 {
		t.Errorf("limit param = %v, want 10", params["limit"])
	}
	// Must filter by language.
	if !querytestutil.SearchString(cypher, "$language") {
		t.Error("cypher should reference $language parameter")
	}
	// Must filter by repo.
	if !querytestutil.SearchString(cypher, "$repo_id") {
		t.Error("cypher should reference $repo_id parameter")
	}
	// Must filter by name.
	if !querytestutil.SearchString(cypher, "$query") {
		t.Error("cypher should reference $query parameter")
	}
	if !querytestutil.SearchString(cypher, "e.type_annotation_count as type_annotation_count") {
		t.Error("cypher should project type_annotation_count")
	}
	if !querytestutil.SearchString(cypher, "e.type_annotation_kinds as type_annotation_kinds") {
		t.Error("cypher should project type_annotation_kinds")
	}
}

func TestBuildLanguageCypher_Repository(t *testing.T) {
	cypher, params := buildLanguageCypher("go", "Repository", "", "", 25)

	if !querytestutil.SearchString(cypher, "Repository") {
		t.Error("cypher should contain Repository label")
	}
	// The Repository builder binds the same spelling list as the other three,
	// so a csharp or typescript query reaches c_sharp and tsx rows (#6546).
	if !querytestutil.SearchString(cypher, "f.language IN $languages") {
		t.Error("cypher should filter on f.language IN $languages")
	}
	if querytestutil.SearchString(cypher, "$language_title") {
		t.Error("cypher must not carry the retired $language_title equality")
	}
	if got, ok := params["languages"].([]string); !ok || !slices.Contains(got, "go") {
		t.Errorf("params[languages] = %#v, want a list carrying go", params["languages"])
	}
	if params["limit"] != 25 {
		t.Errorf("limit param = %v, want 25", params["limit"])
	}
	// No repo_id or query filters when empty.
	if _, ok := params["repo_id"]; ok {
		t.Error("repo_id should not be set when empty")
	}
	if _, ok := params["query"]; ok {
		t.Error("query should not be set when empty")
	}
}

func TestBuildLanguageCypher_FunctionDoesNotDuplicateRepoNameAlias(t *testing.T) {
	cypher, _ := buildLanguageCypher("python", "Function", "handler", "repo-1", 10)

	if got, want := strings.Count(cypher, " as repo_name"), 1; got != want {
		t.Fatalf("strings.Count(cypher, \" as repo_name\") = %d, want %d; cypher=%q", got, want, cypher)
	}
	if strings.Contains(cypher, "e.repo_name as repo_name") {
		t.Fatalf("cypher = %q, must not alias entity repo_name onto the canonical repo_name column", cypher)
	}
}

func TestBuildLanguageCypher_AllEntityTypes(t *testing.T) {
	// Verify all entity types produce valid cypher.
	for typeName, label := range graphBackedEntityTypes {
		cypher, params := buildLanguageCypher("python", label, "", "", 10)
		if cypher == "" {
			t.Errorf("entity type %q produced empty cypher", typeName)
		}
		if got := querytestutil.BoundCanonicalLanguage(t, params); got != "python" {
			t.Errorf("entity type %q: bound canonical language = %v", typeName, got)
		}
	}
}

func TestJoinKeys(t *testing.T) {
	m := map[string]bool{"c": true, "a": true, "b": true}
	got := joinKeys(m)
	if got != "a, b, c" {
		t.Errorf("joinKeys = %q, want %q", got, "a, b, c")
	}
}

func TestSortStrings(t *testing.T) {
	s := []string{"go", "c", "rust", "java", "dart"}
	sortStrings(s)
	for i := 1; i < len(s); i++ {
		if s[i] < s[i-1] {
			t.Errorf("not sorted at index %d: %v", i, s)
		}
	}
}

// TestBuildLanguageCypher_JSXBindsJavaScriptSpellings: a jsx request is a
// javascript request whose bound spelling list still reaches jsx-stamped
// rows. The predicate is `language IN $languages`; there is no extension
// fallback to carry the alias (#6546).
func TestBuildLanguageCypher_JSXBindsJavaScriptSpellings(t *testing.T) {
	cypher, params := buildLanguageCypher("jsx", "File", "Button", "", 5)

	if got, want := querytestutil.BoundCanonicalLanguage(t, params), "javascript"; got != want {
		t.Fatalf("bound canonical language = %#v, want %#v", got, want)
	}
	if !querytestutil.SearchString(cypher, "f.language IN $languages") {
		t.Fatalf("buildLanguageCypher(\"jsx\") missing the spelling-list predicate in %q", cypher)
	}
	assertLanguageSpellingsBound(t, params, "javascript", "jsx")
}

func TestBuildLanguageCypher_TSXBindsTypeScriptSpellings(t *testing.T) {
	cypher, params := buildLanguageCypher("tsx", "File", "Component", "", 5)

	if got, want := querytestutil.BoundCanonicalLanguage(t, params), "typescript"; got != want {
		t.Fatalf("bound canonical language = %#v, want %#v", got, want)
	}
	if !querytestutil.SearchString(cypher, "f.language IN $languages") {
		t.Fatalf("buildLanguageCypher(\"tsx\") missing the spelling-list predicate in %q", cypher)
	}
	assertLanguageSpellingsBound(t, params, "typescript", "tsx")
}

// The File and Directory builder shape tests live here too (moved from
// language_query_builder_test.go, #6642), which existed only to keep
// language_queries_test.go under the repository's 500-line cap.

func TestBuildLanguageCypher_File(t *testing.T) {
	cypher, params := buildLanguageCypher("rust", "File", "main", "", 10)

	if !querytestutil.SearchString(cypher, "File") {
		t.Error("cypher should contain File label")
	}
	// The language filter is the property predicate alone; no extension
	// fallback is spliced into the WHERE (#6546).
	if !querytestutil.SearchString(cypher, "f.language IN $languages") {
		t.Error("cypher should filter on f.language IN $languages")
	}
	if querytestutil.SearchString(cypher, "ENDS WITH") {
		t.Error("cypher must not carry an ENDS WITH extension fallback")
	}
	if got, ok := params["languages"].([]string); !ok || !slices.Contains(got, "rust") {
		t.Errorf("params[languages] = %#v, want a list carrying rust", params["languages"])
	}
	if params["query"] != "main" {
		t.Errorf("query param = %v, want main", params["query"])
	}
}

func TestBuildLanguageCypher_Directory(t *testing.T) {
	cypher, _ := buildLanguageCypher("java", "Directory", "", "repo:x", 5)

	if !querytestutil.SearchString(cypher, "Directory") {
		t.Error("cypher should contain Directory label")
	}
	if !querytestutil.SearchString(cypher, "f.language IN $languages") {
		t.Error("cypher should filter on f.language IN $languages")
	}
	if querytestutil.SearchString(cypher, "ENDS WITH") {
		t.Error("cypher must not carry an ENDS WITH extension fallback")
	}
}
