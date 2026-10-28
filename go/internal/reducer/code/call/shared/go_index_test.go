// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// goModEnvelope builds a parsed go.mod fact declaring modulePath at the
// repository root.
func goModEnvelope(repoID, modulePath string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": "go.mod",
			"parsed_file_data": map[string]any{
				"path": "/repo/go.mod",
				"lang": "gomod",
				"variables": []any{
					map[string]any{"config_kind": "module_declaration", "value": modulePath},
				},
			},
		},
	}
}

// goSourceEnvelope builds a parsed Go source fact with the given function
// rows in package dir.
func goSourceEnvelope(repoID, dir, name string, functions ...map[string]any) facts.Envelope {
	items := make([]any, 0, len(functions))
	for _, function := range functions {
		items = append(items, function)
	}
	relative := name
	if dir != "" {
		relative = dir + "/" + name
	}
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relative,
			"parsed_file_data": map[string]any{
				"path":      "/repo/" + relative,
				"lang":      "go",
				"functions": items,
			},
		},
	}
}

// TestBuildGoCrossRepoExportIndex proves unique exports resolve, forked
// duplicates stay ambiguous, and methods plus unexported names never enter
// the surface.
func TestBuildGoCrossRepoExportIndex(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		goModEnvelope("repo-a", "example.com/mod"),
		goSourceEnvelope("repo-a", "pkg", "serve.go",
			map[string]any{"name": "Serve", "uid": "entity-serve"},
			map[string]any{"name": "serve", "uid": "entity-unexported"},
			map[string]any{"name": "Run", "uid": "entity-method", "receiver_type": "Worker"},
			map[string]any{"name": "", "uid": "entity-noname"},
			map[string]any{"name": "Noid"},
		),
		goModEnvelope("repo-b", "example.com/mod"),
		goSourceEnvelope("repo-b", "pkg", "serve.go",
			map[string]any{"name": "Serve", "uid": "entity-serve-fork"},
		),
		goModEnvelope("repo-c", "example.com/other"),
		goSourceEnvelope("repo-c", "", "solo.go",
			map[string]any{"name": "Solo", "uid": "entity-solo"},
		),
	}
	index := buildGoCrossRepoExportIndex(envelopes)
	ambiguous := index["example.com/mod/pkg"]["Serve"]
	if ambiguous.Count != 2 || ambiguous.EntityID != "" {
		t.Fatalf("forked Serve entry = %#v, want count 2 without entity", ambiguous)
	}
	solo := index["example.com/other"]["Solo"]
	if solo.Count != 1 || solo.EntityID != "entity-solo" || solo.RepositoryID != "repo-c" {
		t.Fatalf("Solo entry = %#v, want the single entity-solo", solo)
	}
	if _, ok := index["example.com/mod/pkg"]["serve"]; ok {
		t.Fatal("unexported name entered the export surface, want absent")
	}
	if _, ok := index["example.com/mod/pkg"]["Run"]; ok {
		t.Fatal("method receiver entered the export surface, want absent")
	}
	if got := buildGoCrossRepoExportIndex(nil); got != nil {
		t.Fatalf("empty build = %#v, want nil", got)
	}
}

// TestGoImportPathForFile proves the deepest containing module root wins
// and other repositories never match.
func TestGoImportPathForFile(t *testing.T) {
	t.Parallel()

	roots := []goModuleRoot{
		{repositoryID: "repo", relativeDir: ".", modulePath: "example.com/mod"},
		{repositoryID: "repo", relativeDir: "nested", modulePath: "example.com/nested"},
		{repositoryID: "other", relativeDir: ".", modulePath: "example.com/other"},
	}
	if got := goImportPathForFile(roots, "repo", "pkg/serve.go"); got != "example.com/mod/pkg" {
		t.Fatalf("import path = %q, want example.com/mod/pkg", got)
	}
	if got := goImportPathForFile(roots, "repo", "nested/serve.go"); got != "example.com/nested" {
		t.Fatalf("nested import path = %q, want example.com/nested", got)
	}
	if got := goImportPathForFile(roots, "repo", ""); got != "" {
		t.Fatalf("blank file import path = %q, want empty", got)
	}
	if got := goImportPathForFile(roots, "missing", "pkg/serve.go"); got != "" {
		t.Fatalf("foreign repo import path = %q, want empty", got)
	}
}

// TestGoDirectoryWithin pins root containment and relative remainder.
func TestGoDirectoryWithin(t *testing.T) {
	t.Parallel()

	if within, ok := goDirectoryWithin("", "pkg"); !ok || within != "pkg" {
		t.Fatalf("empty root = (%q, %v), want (pkg, true)", within, ok)
	}
	if within, ok := goDirectoryWithin("pkg", "pkg"); !ok || within != "" {
		t.Fatalf("same dir = (%q, %v), want (empty, true)", within, ok)
	}
	if within, ok := goDirectoryWithin("pkg", "pkg/sub"); !ok || within != "sub" {
		t.Fatalf("subdir = (%q, %v), want (sub, true)", within, ok)
	}
	if _, ok := goDirectoryWithin("pkg", "other"); ok {
		t.Fatal("sibling dir reported within, want false")
	}
}

// TestFinalizeGoCrossRepoExportIndex proves singletons resolve and
// duplicates keep only their count.
func TestFinalizeGoCrossRepoExportIndex(t *testing.T) {
	t.Parallel()

	candidates := map[string]map[string]map[string]goCrossRepoExportEntry{
		"example.com/mod": {
			"Solo": {"e1": {EntityID: "e1", RepositoryID: "repo"}},
			"Duo": {
				"e1": {EntityID: "e1", RepositoryID: "repo-a"},
				"e2": {EntityID: "e2", RepositoryID: "repo-b"},
			},
		},
	}
	index := finalizeGoCrossRepoExportIndex(candidates)
	if got := index["example.com/mod"]["Solo"]; got.Count != 1 || got.EntityID != "e1" {
		t.Fatalf("Solo = %#v, want count 1 with e1", got)
	}
	if got := index["example.com/mod"]["Duo"]; got.Count != 2 || got.EntityID != "" {
		t.Fatalf("Duo = %#v, want count 2 without entity", got)
	}
	if got := finalizeGoCrossRepoExportIndex(nil); got != nil {
		t.Fatalf("nil finalize = %#v, want nil", got)
	}
}

// TestGoExportedName pins the uppercase-first-rune export rule.
func TestGoExportedName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Serve", "ServeHTTP", "Écouter", "A"} {
		if !GoExportedName(name) {
			t.Fatalf("GoExportedName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "serve", "  ", "9lives", "_hidden"} {
		if GoExportedName(name) {
			t.Fatalf("GoExportedName(%q) = true, want false", name)
		}
	}
}

// TestGoSourceFile proves lang wins over extension with the .go suffix as
// the unmarked fallback.
func TestGoSourceFile(t *testing.T) {
	t.Parallel()

	if !goSourceFile(map[string]any{"lang": "go"}, "/repo/pkg/serve.go") {
		t.Fatal("go lang file should be Go source")
	}
	if goSourceFile(map[string]any{"lang": "python"}, "/repo/pkg/serve.go") {
		t.Fatal("python lang file must not be Go source")
	}
	if !goSourceFile(map[string]any{}, "/repo/pkg/serve.go") {
		t.Fatal("unmarked .go path should be Go source")
	}
	if goSourceFile(map[string]any{}, "/repo/pkg/serve.py") {
		t.Fatal("unmarked .py path must not be Go source")
	}
}

// TestGoImportPathJoin proves slash-join semantics with the empty-dir
// identity.
func TestGoImportPathJoin(t *testing.T) {
	t.Parallel()

	if got := goImportPathJoin("example.com/mod", "pkg"); got != "example.com/mod/pkg" {
		t.Fatalf("join = %q, want example.com/mod/pkg", got)
	}
	if got := goImportPathJoin("example.com/mod", ""); got != "example.com/mod" {
		t.Fatalf("empty-dir join = %q, want the module path", got)
	}
}

// TestGoModuleDeclaredPath proves typed, variable-value, and variable-name
// module declarations resolve in order.
func TestGoModuleDeclaredPath(t *testing.T) {
	t.Parallel()

	if got := goModuleDeclaredPath(map[string]any{"lang": "python"}); got != "" {
		t.Fatalf("non-gomod = %q, want empty", got)
	}
	valued := map[string]any{
		"lang": "gomod",
		"variables": []any{
			map[string]any{"config_kind": "other", "value": "example.com/skip"},
			map[string]any{"config_kind": "module_declaration", "value": "example.com/mod"},
		},
	}
	if got := goModuleDeclaredPath(valued); got != "example.com/mod" {
		t.Fatalf("variable value = %q, want example.com/mod", got)
	}
	named := map[string]any{
		"lang": "gomod",
		"variables": []any{
			map[string]any{"config_kind": "module_declaration", "name": "example.com/named"},
		},
	}
	if got := goModuleDeclaredPath(named); got != "example.com/named" {
		t.Fatalf("variable name = %q, want example.com/named", got)
	}
	if got := goModuleDeclaredPath(map[string]any{"lang": "gomod"}); got != "" {
		t.Fatalf("bare gomod = %q, want empty", got)
	}
}

// TestAddGoMethodReturnTypeCandidate proves return-type candidates record
// once per type and ignore incomplete rows.
func TestAddGoMethodReturnTypeCandidate(t *testing.T) {
	t.Parallel()

	candidates := map[string]map[string]map[string]struct{}{}
	item := map[string]any{"name": "Run", "class_context": "Worker", "return_type": "Result"}
	addGoMethodReturnTypeCandidate(candidates, "repo", item)
	addGoMethodReturnTypeCandidate(candidates, "repo", item)
	if !reflect.DeepEqual(candidates["repo"]["Worker.Run"], map[string]struct{}{"Result": {}}) {
		t.Fatalf("candidates = %#v, want one Result entry", candidates)
	}
	addGoMethodReturnTypeCandidate(candidates, "", item)
	addGoMethodReturnTypeCandidate(candidates, "repo", map[string]any{"name": "Run"})
	if len(candidates) != 1 || len(candidates["repo"]) != 1 {
		t.Fatalf("incomplete rows polluted candidates = %#v", candidates)
	}
}
