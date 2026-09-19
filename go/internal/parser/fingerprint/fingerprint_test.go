// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"reflect"
	"testing"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func parseOne(t *testing.T, loader func() unsafe.Pointer, src string) (*tree_sitter.Node, []byte) {
	t.Helper()
	p := tree_sitter.NewParser()
	t.Cleanup(func() { p.Close() })
	if err := p.SetLanguage(tree_sitter.NewLanguage(loader())); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	bs := []byte(src)
	tree := p.Parse(bs, nil)
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	t.Cleanup(func() { tree.Close() })
	if tree.RootNode().HasError() {
		t.Fatalf("fixture has parse errors: %q", src)
	}
	return tree.RootNode(), bs
}

// firstFuncBody returns the body node of the first function-like declaration.
func firstFuncBody(t *testing.T, root *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	t.Helper()
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var found *tree_sitter.Node
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if found != nil {
			return
		}
		if want[n.Kind()] {
			found = n
			return
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(root)
	if found == nil {
		t.Fatalf("no %v node found", kinds)
	}
	body := found.ChildByFieldName("body")
	if body == nil {
		t.Fatalf("%s has no body field", found.Kind())
	}
	return body
}

func TestGoExactRenamedCount(t *testing.T) {
	root, src := parseOne(t, tree_sitter_go.Language, `package p
func add(a int, b int) int {
	// sum two numbers
	return a + b + 1
}
`)
	body := firstFuncBody(t, root, "function_declaration")
	got, err := FingerprintBody("go", body, src)
	if err != nil {
		t.Fatalf("FingerprintBody: %v", err)
	}
	if !got.RenamedSupported {
		t.Fatal("go must be a full tier (renamed supported)")
	}
	if got.TokenCount == 0 {
		t.Fatal("expected nonzero token count")
	}
	if got.Exact == "" || got.Renamed == "" {
		t.Fatal("expected nonempty fingerprints")
	}
	if len(got.Sketch) != 128 {
		t.Fatalf("expected 128 sketch registers, got %d", len(got.Sketch))
	}
	if len(got.Bands) != 32 {
		t.Fatalf("expected 32 bands, got %d", len(got.Bands))
	}
	// Determinism: same input, same output.
	again, err := FingerprintBody("go", body, src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatal("fingerprint is not deterministic")
	}
}

func TestGoIgnoresCommentsAndRenamesPositionally(t *testing.T) {
	mk := func(body string) Result {
		root, src := parseOne(t, tree_sitter_go.Language, "package p\nfunc f() int {\n"+body+"\n}\n")
		r, err := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
		if err != nil {
			t.Fatal(err)
		}
		return *r
	}
	withComment := mk("\t// leading comment\n\treturn x + 1\n")
	withoutComment := mk("\treturn x + 1\n")
	if withComment.Exact != withoutComment.Exact {
		t.Fatal("comments must not affect the exact fingerprint")
	}
	renamedIdent := mk("\treturn y + 1\n")
	if withComment.Renamed != renamedIdent.Renamed {
		t.Fatal("alpha-renaming must normalize distinct identifiers positionally")
	}
	renamedLit := mk("\treturn x + 2\n")
	if withComment.Renamed != renamedLit.Renamed {
		t.Fatal("alpha-renaming must normalize distinct literals positionally")
	}
	if withComment.Exact == renamedIdent.Exact {
		t.Fatal("exact fingerprint must distinguish renamed identifiers")
	}
}

func TestFullTierShapes(t *testing.T) {
	cases := []struct {
		lang   string
		loader func() unsafe.Pointer
		src    string
		kinds  []string
	}{
		{
			"python", tree_sitter_python.Language,
			"def f(x):\n    return x + 1\n",
			[]string{"function_definition"},
		},
		{
			"typescript", tree_sitter_typescript.LanguageTypescript,
			"export const f = async (x: number): Promise<number> => { return x + 1; };\n",
			[]string{"variable_declarator"},
		},
		{
			"tsx", tree_sitter_typescript.LanguageTSX,
			"export function* g(items: string[]): Generator<string> { yield items[0]; }\n",
			[]string{"generator_function_declaration"},
		},
		{
			"javascript", tree_sitter_javascript.Language,
			"module.exports = function (a, b) { return a + b; };\n",
			[]string{"function_expression"},
		},
		{
			"java", tree_sitter_java.Language,
			"class A { int f(int x) { return x + 1; } }\n",
			[]string{"method_declaration"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			root, src := parseOne(t, tc.loader, tc.src)
			// variable_declarator carries the value; fingerprint the value body.
			var body *tree_sitter.Node
			if tc.kinds[0] == "variable_declarator" {
				var decl *tree_sitter.Node
				var visit func(n *tree_sitter.Node)
				visit = func(n *tree_sitter.Node) {
					if decl != nil {
						return
					}
					if n.Kind() == "variable_declarator" {
						decl = n
						return
					}
					for i := range n.ChildCount() {
						visit(n.Child(i))
					}
				}
				visit(root)
				if decl == nil {
					t.Fatal("no variable_declarator found")
				}
				body = decl.ChildByFieldName("value").ChildByFieldName("body")
			} else {
				body = firstFuncBody(t, root, tc.kinds...)
			}
			got, err := FingerprintBody(tc.lang, body, src)
			if err != nil {
				t.Fatalf("FingerprintBody(%s): %v", tc.lang, err)
			}
			if !got.RenamedSupported || got.TokenCount == 0 || got.Exact == "" || len(got.Sketch) != 128 {
				t.Fatalf("incomplete full-tier result for %s: %+v", tc.lang, got.TokenCount)
			}
		})
	}
}

func TestUnknownLanguageIsExactOnly(t *testing.T) {
	root, src := parseOne(t, tree_sitter_go.Language, "package p\nfunc f() int {\n\treturn 1\n}\n")
	// "cobol" is wired to no grammar: unknown languages stay exact-only with
	// no comment exclusion. ("rust" is a known exact-only tier since the F1
	// fix, so it no longer exercises the unknown path.)
	got, err := FingerprintBody("cobol", firstFuncBody(t, root, "function_declaration"), src)
	if err != nil {
		t.Fatalf("FingerprintBody: %v", err)
	}
	if got.RenamedSupported {
		t.Fatal("cobol must be exact-only (renamed not supported)")
	}
	if got.Sketch != nil || len(got.Bands) != 0 {
		t.Fatal("exact-only result must carry no sketch or bands")
	}
	if got.Exact == "" || got.TokenCount == 0 {
		t.Fatal("exact-only result must still carry exact hash and count")
	}
}

func TestEmptyBodyIsStable(t *testing.T) {
	root, src := parseOne(t, tree_sitter_go.Language, "package p\nfunc f() {}\n")
	a, err := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("empty body must fingerprint stably")
	}
	if a.TokenCount != 2 {
		t.Fatalf("empty Go body holds only brace leaves, got %d tokens", a.TokenCount)
	}
}
