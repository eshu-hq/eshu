// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// firstExportStatement returns the first export_statement under root.
func firstExportStatement(t *testing.T, root *tree_sitter.Node) *tree_sitter.Node {
	t.Helper()

	var found *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if found == nil && node.Kind() == "export_statement" {
			clone := *node
			found = &clone
		}
	})
	if found == nil {
		t.Fatalf("no export_statement in fixture")
	}
	return found
}

// TestReExportSourceReadsOnlyTheGrammarSourceField is the #7056 regression at
// the helper level: ReExportSource feeds both the imports bucket and the
// dead-code public-surface walk, so a declaration export must yield no module
// source even when " from " appears later in its text.
func TestReExportSourceReadsOnlyTheGrammarSourceField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "class with doc comment",
			body: "export class X {\n  m() { f(a, this.basePage); }\n  /** comes from the upstream service */\n  n() {}\n}\n",
			want: "",
		},
		{
			name: "const with string literal",
			body: "export const s = \"copied from ./x\";\n",
			want: "",
		},
		{
			name: "function with template literal",
			body: "export function g(y) { return `read ${y} from ./cache`; }\n",
			want: "",
		},
		{name: "named reexport double quote", body: "export { a } from \"./a\";\n", want: "./a"},
		{name: "named reexport single quote", body: "export { a } from './a';\n", want: "./a"},
		{name: "star reexport", body: "export * from \"pkg/sub\";\n", want: "pkg/sub"},
		{name: "star as reexport", body: "export * as ns from \"./ns\";\n", want: "./ns"},
		{name: "empty specifier", body: "export * from \"\";\n", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, source, closeFn := parseRootForTest(t, tc.body)
			defer closeFn()

			if got := ReExportSource(firstExportStatement(t, root), source); got != tc.want {
				t.Fatalf("ReExportSource() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReExportSpecifiersIgnoresDeclarationBodies proves the brace-text fallback
// never reads specifiers out of a declaration export's body: the dead-code
// public-surface walk calls ReExportSpecifiers on sourceless export statements
// and would otherwise mint re-exports named after call arguments.
func TestReExportSpecifiersIgnoresDeclarationBodies(t *testing.T) {
	t.Parallel()

	cases := []string{
		"export class X {\n  m() { f(a, this.basePage); }\n}\n",
		"export function g() { return h(a, B); }\n",
		"export const o = { a, b };\n",
		"export default { a, b };\n",
	}
	for _, body := range cases {
		root, source, closeFn := parseRootForTest(t, body)
		got := ReExportSpecifiers(firstExportStatement(t, root), source)
		closeFn()
		if len(got) != 0 {
			t.Fatalf("ReExportSpecifiers(%q) = %#v, want none", body, got)
		}
	}
}

// TestReExportSpecifiersKeepsLocalExportClause pins the sourceless export
// clause the dead-code walk maps back to imported bindings.
func TestReExportSpecifiersKeepsLocalExportClause(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "import { a, b } from \"./m\";\nexport { a, b as c };\n")
	defer closeFn()
	got := ReExportSpecifiers(firstExportStatement(t, root), source)
	if len(got) != 2 || got[0].OriginalName != "a" || got[1].OriginalName != "b" || got[1].ExportedName != "c" {
		t.Fatalf("ReExportSpecifiers() = %#v, want a and b as c", got)
	}
}
