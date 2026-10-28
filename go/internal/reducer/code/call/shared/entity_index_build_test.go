// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// entityIndexEnvelope builds a file fact envelope with the given parsed
// file data.
func entityIndexEnvelope(repoID, relativePath string, fileData map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"parsed_file_data": fileData,
		},
	}
}

// TestBuildEntityIndexTypes proves class/struct/interface/alias buckets
// index with their entity kinds, spans, and unique names while
// non-file facts and body-less files stay out.
func TestBuildEntityIndexTypes(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		entityIndexEnvelope("repo-a", "pkg/serve.go", map[string]any{
			"path":              "/repo/pkg/serve.go",
			"lang":              "go",
			"node_package_name": "pkg",
			"functions": []any{
				map[string]any{"uid": "e-serve", "name": "Serve", "line_number": 10, "end_line": 20},
				map[string]any{"uid": "e-ctor", "name": "constructor", "class_context": "Server", "line_number": 30},
				map[string]any{"uid": "", "name": "Noname", "line_number": 40},
				map[string]any{"name": "Noline"},
			},
			"classes":      []any{map[string]any{"uid": "e-server", "name": "Server", "line_number": 5, "end_line": 50}},
			"structs":      []any{map[string]any{"uid": "e-cfg", "name": "Config", "line_number": 60}},
			"interfaces":   []any{map[string]any{"uid": "e-svc", "name": "IService", "line_number": 70}},
			"type_aliases": []any{map[string]any{"uid": "e-alias", "name": "MyString", "line_number": 80}},
		}),
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		entityIndexEnvelope("repo-a", "pkg/empty.go", map[string]any{"path": "/repo/pkg/empty.go"}),
		{FactKind: "file", Payload: map[string]any{"repo_id": "repo-a"}},
	}
	index := BuildEntityIndex(envelopes)
	for entityID, kind := range map[string]string{
		"e-server": "Class", "e-cfg": "Struct", "e-svc": "Interface", "e-alias": "TypeAlias",
	} {
		if got := index.entityTypeByID[entityID]; got != kind {
			t.Fatalf("entity type of %s = %q, want %s", entityID, got, kind)
		}
		if got := index.EntityFileByID(entityID); got != "pkg/serve.go" {
			t.Fatalf("entity file of %s = %q, want pkg/serve.go", entityID, got)
		}
	}
	if got := index.UniqueNameByRepo("repo-a", "Server"); got != "e-server" {
		t.Fatalf("UniqueNameByRepo Server = %q, want e-server", got)
	}
	if got := index.UniqueNameByPath("/repo/pkg/serve.go", "Serve"); got != "e-serve" {
		t.Fatalf("UniqueNameByPath Serve = %q, want e-serve", got)
	}
	if !index.RepoPublishesNodePackage("repo-a", "pkg") {
		t.Fatal("node package unpublished, want published")
	}
	spans := index.SpansByFile("repo-a", "/repo/pkg/serve.go")
	found := false
	for _, span := range spans {
		if span.EntityID == "e-serve" && span.StartLine == 10 && span.EndLine == 20 {
			found = true
		}
	}
	if !found {
		t.Fatalf("serve span missing from %#v", spans)
	}
	if got := ResolveEntityID(index, "repo-a", "/repo/pkg/serve.go", 10); got != "e-serve" {
		t.Fatalf("ResolveEntityID line 10 = %q, want e-serve", got)
	}
	if got := ResolveEntityID(index, "repo-a", "/repo/pkg/serve.go", 0); got != "" {
		t.Fatalf("ResolveEntityID line 0 = %q, want empty", got)
	}
	if got := ResolveEntityID(index, "repo-a", "", 10); got != "" {
		t.Fatalf("ResolveEntityID blank path = %q, want empty", got)
	}
	if got := ResolveSameFileCalleeEntityID(index, "repo-a", "/repo/pkg/serve.go", "pkg/serve.go",
		map[string]any{"name": "Serve", "full_name": "Serve"}); got != "e-serve" {
		t.Fatalf("same-file Serve = %q, want e-serve", got)
	}
	if got := ResolveSameFileCalleeEntityID(index, "repo-a", "/repo/pkg/serve.go", "pkg/serve.go",
		map[string]any{"name": "Missing", "full_name": "Missing"}); got != "" {
		t.Fatalf("same-file Missing = %q, want empty", got)
	}
	if got := ResolveSameFileCalleeEntityID(index, "repo-a", "/repo/pkg/serve.go", "pkg/serve.go",
		map[string]any{"name": "run", "full_name": "other.run"}); got != "" {
		t.Fatalf("same-file qualified miss = %q, want empty without broad fallback", got)
	}
}

// TestBuildEntityIndexEmpty proves empty input builds a usable index that
// resolves nothing.
func TestBuildEntityIndexEmpty(t *testing.T) {
	t.Parallel()

	index := BuildEntityIndex(nil)
	if got := ResolveEntityID(index, "repo", "/repo/a.go", 1); got != "" {
		t.Fatalf("empty ResolveEntityID = %q, want empty", got)
	}
	if index.HasGoExports() {
		t.Fatal("empty index reports Go exports, want none")
	}
}
