// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// parseTSRootForTest parses a TypeScript source snippet into a root node and
// source bytes. The caller must invoke the returned close function.
func parseTSRootForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()

	language := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatalf("Parse() returned nil tree")
	}
	return tree.RootNode(), bytes, func() {
		tree.Close()
		parser.Close()
	}
}

// firstNodeOfKind returns the first node of one of the given kinds under root.
func firstNodeOfKind(t *testing.T, root *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	t.Helper()

	want := map[string]struct{}{}
	for _, kind := range kinds {
		want[kind] = struct{}{}
	}
	var found *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if found != nil {
			return
		}
		if _, ok := want[node.Kind()]; ok {
			clone := *node
			found = &clone
		}
	})
	if found == nil {
		t.Fatalf("no node of kinds %v in fixture", kinds)
	}
	return found
}

// TestReExportAttributeEntriesShapes pins the recovered rows for each
// grammar's error shape (issue #7059): the JavaScript grammar surfaces the
// broken export as a top-level ERROR node, the TypeScript grammar as an
// "export"-labeled statement holding the ERROR.
func TestReExportAttributeEntriesShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		typescript bool
		body       string
		wantName   string
		wantSource string
	}{
		{name: "js star with", body: "export * from './x.json' with { type: 'json' };\n", wantName: "*", wantSource: "./x.json"},
		{name: "js named with", body: "export { a } from './x.json' with { type: 'json' };\n", wantName: "a", wantSource: "./x.json"},
		{name: "js named assert", body: "export { a } from './x.json' assert { type: 'json' };\n", wantName: "a", wantSource: "./x.json"},
		{name: "ts star with", typescript: true, body: "export * from './x.json' with { type: 'json' };\n", wantName: "*", wantSource: "./x.json"},
		{name: "ts named with", typescript: true, body: "export { a, b as c } from './x.json' with { type: 'json' };\n", wantName: "a", wantSource: "./x.json"},
		{name: "ts star assert", typescript: true, body: "export * from './x.json' assert { type: 'json' };\n", wantName: "*", wantSource: "./x.json"},
		{name: "ts namespace with", typescript: true, body: "export * as ns from './x.json' with { type: 'json' };\n", wantName: "*", wantSource: "./x.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var root *tree_sitter.Node
			var source []byte
			var closeFn func()
			if tc.typescript {
				root, source, closeFn = parseTSRootForTest(t, tc.body)
			} else {
				root, source, closeFn = parseRootForTest(t, tc.body)
			}
			defer closeFn()

			var node *tree_sitter.Node
			if tc.typescript {
				node = firstNodeOfKind(t, root, "labeled_statement")
			} else {
				node = firstNodeOfKind(t, root, "ERROR")
			}
			got := ReExportAttributeEntries(node, source, "typescript")
			if len(got) == 0 {
				t.Fatalf("ReExportAttributeEntries(%q) = none, want %q from %q", tc.body, tc.wantName, tc.wantSource)
			}
			matched := false
			for _, item := range got {
				name, _ := item["name"].(string)
				source, _ := item["source"].(string)
				typ, _ := item["import_type"].(string)
				if name == tc.wantName && source == tc.wantSource && typ == "reexport" {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("ReExportAttributeEntries(%q) = %#v, want name %q source %q", tc.body, got, tc.wantName, tc.wantSource)
			}
		})
	}
}

// TestReExportAttributeEntriesRejects proves the recovery stays silent outside
// its two shapes: declaration exports (the #7056 false-positive family),
// non-relative specifiers, invalid bare exports without a star marker, and
// ordinary labeled statements all yield nothing.
func TestReExportAttributeEntriesRejects(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export class X {\n  m() { return 1; }\n}\n")
	defer closeFn()
	decl := firstNodeOfKind(t, root, "export_statement")
	if got := ReExportAttributeEntries(decl, source, "javascript"); len(got) != 0 {
		t.Fatalf("declaration export = %#v, want none", got)
	}

	rejectBodies := []struct {
		name       string
		typescript bool
		body       string
		kind       string
	}{
		{name: "non-relative star", body: "export * from \"lodash\" with { type: 'json' };\n", kind: "ERROR"},
		{name: "bare export without star", body: "export foo from \"./x\";\n", kind: "ERROR"},
		{name: "ordinary label", body: "loop: for (const x of y) { break loop; }\n", kind: "labeled_statement"},
		{name: "ts non-relative named", typescript: true, body: "export { a } from \"lodash\" with { type: 'json' };\n", kind: "labeled_statement"},
	}
	for _, tc := range rejectBodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var root *tree_sitter.Node
			var source []byte
			var closeFn func()
			if tc.typescript {
				root, source, closeFn = parseTSRootForTest(t, tc.body)
			} else {
				root, source, closeFn = parseRootForTest(t, tc.body)
			}
			defer closeFn()

			node := firstNodeOfKind(t, root, tc.kind)
			if got := ReExportAttributeEntries(node, source, "javascript"); len(got) != 0 {
				t.Fatalf("ReExportAttributeEntries(%q) = %#v, want none", tc.body, got)
			}
		})
	}
}

// TestImportEntriesHandlesImportEquals pins the TypeScript import-equals row
// at the extractor level (issue #7059): the specifier lives on the
// import_require_clause child, and the entity-name form yields nothing.
func TestImportEntriesHandlesImportEquals(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "import x = require('./x');\n")
	defer closeFn()
	node := firstNodeOfKind(t, root, "import_statement")
	got := ImportEntries(node, source, "typescript")
	if len(got) != 1 {
		t.Fatalf("ImportEntries(import-equals) = %#v, want one row", got)
	}
	item := got[0]
	if name, _ := item["name"].(string); name != "*" {
		t.Fatalf("name = %q, want %q", name, "*")
	}
	if alias, _ := item["alias"].(string); alias != "x" {
		t.Fatalf("alias = %q, want %q", alias, "x")
	}
	if spec, _ := item["source"].(string); spec != "./x" {
		t.Fatalf("source = %q, want %q", spec, "./x")
	}
	if typ, _ := item["import_type"].(string); typ != "require" {
		t.Fatalf("import_type = %q, want %q", typ, "require")
	}

	// The entity-name form parses as a bare import_alias, never reaching
	// ImportEntries through the import_statement walk; the kind guard pins
	// that even a direct call yields nothing.
	entityRoot, entitySource, entityClose := parseTSRootForTest(t, "import x = A.B.C;\n")
	defer entityClose()
	entity := firstNodeOfKind(t, entityRoot, "import_alias")
	if got := ImportEntries(entity, entitySource, "typescript"); len(got) != 0 {
		t.Fatalf("ImportEntries(entity-name) = %#v, want none", got)
	}
}
