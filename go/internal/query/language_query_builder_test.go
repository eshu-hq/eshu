// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"testing"
)

// The File and Directory builder shape tests live here rather than in
// language_queries_test.go so that file stays under the 500-line cap; the
// Repository and entity builder tests remain there next to the handler tests.

func TestBuildLanguageCypher_File(t *testing.T) {
	cypher, params := buildLanguageCypher("rust", "File", "main", "", 10)

	if !searchString(cypher, "File") {
		t.Error("cypher should contain File label")
	}
	// The language filter is the property predicate alone; no extension
	// fallback is spliced into the WHERE (#6546).
	if !searchString(cypher, "f.language IN $languages") {
		t.Error("cypher should filter on f.language IN $languages")
	}
	if searchString(cypher, "ENDS WITH") {
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

	if !searchString(cypher, "Directory") {
		t.Error("cypher should contain Directory label")
	}
	if !searchString(cypher, "f.language IN $languages") {
		t.Error("cypher should filter on f.language IN $languages")
	}
	if searchString(cypher, "ENDS WITH") {
		t.Error("cypher must not carry an ENDS WITH extension fallback")
	}
}
