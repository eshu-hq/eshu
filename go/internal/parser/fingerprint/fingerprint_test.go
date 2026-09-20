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
	// Body stays above MinTokenCount: FingerprintBody skips renamed/sketch
	// output below the floor, so a small fixture could not pin the full
	// shape this test exists to cover.
	root, src := parseOne(t, tree_sitter_go.Language, `package p
func add(a int, b int) int {
	// sum two numbers
	c := a + b + 1
	d := c * 2 - a
	e := d + b + 3
	f := e * a - d
	g := f + c + 5
	h := g * b - e
	i := h + d + 7
	j := i * c - f
	k := j + e + 9
	m := k * h - g
	n := m + i + 11
	return n + j + 12
}
`)
	body := firstFuncBody(t, root, "function_declaration")
	got := FingerprintBody("go", body, src)
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
	again := FingerprintBody("go", body, src)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("fingerprint is not deterministic")
	}
}

func TestGoIgnoresCommentsAndRenamesPositionally(t *testing.T) {
	// Padding keeps every fixture above MinTokenCount (identical on all
	// sides, comment-free so it cannot disturb the invariance pin) because
	// FingerprintBody emits no renamed output below the floor.
	const pad = "\tp1 := 101\n\tp2 := 102\n\tp3 := 103\n\tp4 := 104\n" +
		"\tp5 := 105\n\tp6 := 106\n\tp7 := 107\n\tp8 := 108\n" +
		"\tp9 := 109\n\tp10 := 110\n\tp11 := 111\n\tp12 := 112\n" +
		"\tp13 := 113\n\tp14 := 114\n\tp15 := 115\n\tp16 := 116\n"
	mk := func(body string) Result {
		root, src := parseOne(t, tree_sitter_go.Language, "package p\nfunc f() int {\n"+body+pad+"\n}\n")
		r := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
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
			"def f(x):\n    a = x + 1\n    b = a + 2\n    c = b + 3\n    d = c + 4\n    e = d + 5\n    f = e + 6\n    g = f + 7\n    h = g + 8\n    i = h + 9\n    j = i + 10\n    k = j + 11\n    return k + 12\n",
			[]string{"function_definition"},
		},
		{
			"typescript", tree_sitter_typescript.LanguageTypescript,
			"export const f = async (x: number): Promise<number> => { const a = x + 1; const b = a + 2; const c = b + 3; const d = c + 4; const e = d + 5; const f = e + 6; const g = f + 7; const h = g + 8; const i = h + 9; const j = i + 10; const k = j + 11; return k + 12; };\n",
			[]string{"variable_declarator"},
		},
		{
			"tsx", tree_sitter_typescript.LanguageTSX,
			"export function* g(items: string[]): Generator<string> { yield items[0]; yield items[1]; yield items[2]; yield items[3]; yield items[4]; yield items[5]; yield items[6]; yield items[7]; yield items[8]; yield items[9]; yield items[10]; yield items[11]; }\n",
			[]string{"generator_function_declaration"},
		},
		{
			"javascript", tree_sitter_javascript.Language,
			"module.exports = function (a, b) { const c = a + b; const d = c + 1; const e = d + 2; const f = e + 3; const g = f + 4; const h = g + 5; const i = h + 6; const j = i + 7; const k = j + 8; const l = k + 9; const m = l + 10; return m + 11; };\n",
			[]string{"function_expression"},
		},
		{
			"java", tree_sitter_java.Language,
			"class A { int f(int x) { int a = x + 1; int b = a + 2; int c = b + 3; int d = c + 4; int e = d + 5; int f = e + 6; int g = f + 7; int h = g + 8; int i = h + 9; int j = i + 10; int k = j + 11; return k + 12; } }\n",
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
			got := FingerprintBody(tc.lang, body, src)
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
	got := FingerprintBody("cobol", firstFuncBody(t, root, "function_declaration"), src)
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
	a := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
	b := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("empty body must fingerprint stably")
	}
	if a.TokenCount != 2 {
		t.Fatalf("empty Go body holds only brace leaves, got %d tokens", a.TokenCount)
	}
}

// TestBelowFloorSkipsSketchAndBands pins the floor fast path: a full-tier
// body under MinTokenCount still yields its exact hash and token count, but
// no renamed hash, sketch, or bands — Attach discards sub-floor results, so
// generating them would be pure waste at corpus scale.
func TestBelowFloorSkipsSketchAndBands(t *testing.T) {
	root, src := parseOne(t, tree_sitter_go.Language, "package p\nfunc f() int {\n\treturn 1\n}\n")
	got := FingerprintBody("go", firstFuncBody(t, root, "function_declaration"), src)
	if got.TokenCount == 0 || got.TokenCount >= MinTokenCount {
		t.Fatalf("fixture must stay below the %d-token floor, got %d tokens", MinTokenCount, got.TokenCount)
	}
	if got.Exact == "" {
		t.Fatal("below-floor result must still carry the exact hash")
	}
	if got.RenamedSupported || got.Renamed != "" || got.Sketch != nil || len(got.Bands) != 0 {
		t.Fatalf("below-floor result must carry no renamed/sketch/bands: %+v", got)
	}
}
