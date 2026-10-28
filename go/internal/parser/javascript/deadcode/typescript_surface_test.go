// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestPropagateReexportStarAndNamed(t *testing.T) {
	t.Parallel()

	star := javaScriptTypeScriptSurfaceWalkItem{path: "p", star: true}
	names, nextStar, ok := javaScriptTypeScriptPropagateReexport(star, javaScriptTypeScriptSurfaceReexport{exportedName: "*", originalName: "*"})
	if len(names) != 0 || !nextStar || !ok {
		t.Errorf("star/star = (%v, %v, %v), want (empty, true, true)", names, nextStar, ok)
	}
	names, nextStar, ok = javaScriptTypeScriptPropagateReexport(star, javaScriptTypeScriptSurfaceReexport{exportedName: "A", originalName: "B"})
	if !ok || nextStar || len(names) != 1 {
		t.Fatalf("star/named = (%v, %v, %v), want one name, non-star, ok", names, nextStar, ok)
	}
	if _, present := names["B"]; !present {
		t.Errorf("star/named propagates %v, want the original name B", names)
	}
	if _, _, ok := javaScriptTypeScriptPropagateReexport(star, javaScriptTypeScriptSurfaceReexport{exportedName: "A"}); ok {
		t.Error("star/named with blank original reported ok")
	}

	item := javaScriptTypeScriptSurfaceWalkItem{path: "p", names: map[string]struct{}{"A": {}}}
	names, nextStar, ok = javaScriptTypeScriptPropagateReexport(item, javaScriptTypeScriptSurfaceReexport{exportedName: "*", originalName: "*"})
	if !ok || nextStar || !reflect.DeepEqual(names, item.names) {
		t.Errorf("names/star = (%v, %v, %v), want cloned names, non-star, ok", names, nextStar, ok)
	}
	names, _, ok = javaScriptTypeScriptPropagateReexport(item, javaScriptTypeScriptSurfaceReexport{exportedName: "A", originalName: "Z"})
	if !ok || len(names) != 1 {
		t.Fatalf("names/matching = (%v, %v), want original Z, ok", names, ok)
	}
	if _, present := names["Z"]; !present {
		t.Errorf("names/matching propagates %v, want Z", names)
	}
	if _, _, ok := javaScriptTypeScriptPropagateReexport(item, javaScriptTypeScriptSurfaceReexport{exportedName: "Q", originalName: "Z"}); ok {
		t.Error("names/missing reported ok")
	}
	empty := javaScriptTypeScriptSurfaceWalkItem{path: "p"}
	if _, _, ok := javaScriptTypeScriptPropagateReexport(empty, javaScriptTypeScriptSurfaceReexport{exportedName: "A", originalName: "A"}); ok {
		t.Error("empty names reported ok")
	}
}

func TestMarkPublicNamesStarAndFilter(t *testing.T) {
	t.Parallel()

	exported := map[string]struct{}{"A": {}, "B": {}}
	public := map[string]struct{}{}
	javaScriptTypeScriptMarkPublicNames(public, exported, nil, true)
	if len(public) != 2 {
		t.Errorf("star marks %v, want both exported names", public)
	}

	public = map[string]struct{}{}
	javaScriptTypeScriptMarkPublicNames(public, exported, map[string]struct{}{"A": {}, "Z": {}}, false)
	if len(public) != 1 {
		t.Fatalf("filtered marks %v, want only A", public)
	}
	if _, ok := public["A"]; !ok {
		t.Errorf("filtered marks %v, want A", public)
	}
}

func TestSurfaceWalkKeyIsStableAndScoped(t *testing.T) {
	t.Parallel()

	star := javaScriptTypeScriptSurfaceWalkItem{path: "pkg/index.ts", star: true}
	if key := javaScriptTypeScriptSurfaceWalkKey(star); len(key) < 3 || key[len(key)-2:] != "|*" {
		t.Errorf("star key = %q, want a |* suffix", key)
	}
	named := javaScriptTypeScriptSurfaceWalkItem{path: "pkg/index.ts", names: map[string]struct{}{"B": {}, "A": {}}}
	first, second := javaScriptTypeScriptSurfaceWalkKey(named), javaScriptTypeScriptSurfaceWalkKey(named)
	if first != second {
		t.Errorf("walk key unstable: %q vs %q", first, second)
	}
	if first == javaScriptTypeScriptSurfaceWalkKey(star) {
		t.Error("named and star keys collide")
	}

	cloned := cloneJavaScriptTypeScriptSurfaceNames(named.names)
	cloned["C"] = struct{}{}
	if _, ok := named.names["C"]; ok {
		t.Error("clone shares storage with the original")
	}
}

func TestIsTypeScriptInterfaceImplementationMethod(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `interface Store { get(): string; }
class SqlStore implements Store {
  get(): string { return "x"; }
  private secret(): void {}
  constructor() {}
}
class Plain {
  get(): string { return "y"; }
}
`)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_definition" {
			return
		}
		name := node.ChildByFieldName("name")
		nameText := ""
		if name != nil {
			nameText = string(source[name.StartByte():name.EndByte()])
		}
		results[nameText] = javaScriptIsTypeScriptInterfaceImplementationMethod(node, nameText, source, parents)
	})
	// Two get methods share one map key (implements vs plain), so the
	// count below is the real assertion; the map pins the unique names.
	if results["secret"] || results["constructor"] {
		t.Errorf("method results = %v, want secret and constructor false", results)
	}
	trues := 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_definition" {
			return
		}
		name := node.ChildByFieldName("name")
		nameText := ""
		if name != nil {
			nameText = string(source[name.StartByte():name.EndByte()])
		}
		if javaScriptIsTypeScriptInterfaceImplementationMethod(node, nameText, source, parents) {
			trues++
		}
	})
	if trues != 1 {
		t.Errorf("%d interface-implementation methods, want exactly 1", trues)
	}
	if javaScriptIsTypeScriptInterfaceImplementationMethod(nil, "get", source, parents) {
		t.Error("nil node reported as implementation")
	}

	var method *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if method == nil && node.Kind() == "method_definition" {
			method = node
		}
	})
	if javaScriptIsTypeScriptInterfaceImplementationMethod(method, "  ", source, parents) {
		t.Error("blank name reported as implementation")
	}
	if javaScriptClassHasImplementsClause(nil) {
		t.Error("nil class reported an implements clause")
	}
}

func TestExportedDeclarationNamesAndDeclarationName(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export function run(): void {}
export class Service {}
export interface Store {}
export type Alias = string;
export enum Mode { A }
export const version = 1;
function hidden(): void {}
`)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	names := javaScriptTypeScriptExportedDeclarationNames(root, source, parents)
	for _, want := range []string{"run", "Service", "Store", "Alias", "Mode", "version"} {
		if _, ok := names[want]; !ok {
			t.Errorf("exported names = %v, want %q", names, want)
		}
	}
	if _, ok := names["hidden"]; ok {
		t.Errorf("exported names = %v, want no hidden", names)
	}

	// Every declaration kind names itself; other node kinds name nothing.
	// First-seen wins: run precedes hidden in the fixture.
	kinded := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "class_declaration", "interface_declaration",
			"type_alias_declaration", "enum_declaration", "variable_declarator", "call_expression":
			if _, seen := kinded[node.Kind()]; !seen {
				kinded[node.Kind()] = javaScriptTypeScriptDeclarationName(node, source)
			}
		}
	})
	for kind, want := range map[string]string{
		"function_declaration": "run", "class_declaration": "Service",
		"interface_declaration": "Store", "type_alias_declaration": "Alias",
		"enum_declaration": "Mode", "variable_declarator": "version",
		"call_expression": "",
	} {
		if kinded[kind] != want {
			t.Errorf("declaration name for %s = %q, want %q", kind, kinded[kind], want)
		}
	}
	if got := javaScriptTypeScriptDeclarationName(nil, source); got != "" {
		t.Errorf("nil node name = %q, want empty", got)
	}
}

func TestStaticReexportsFromRootEdges(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, "export { A, B as C } from \"./mod\";\nexport * from \"./star\";\nexport { Local };\n")
	defer closeFn()

	edges := javaScriptTypeScriptStaticReexportsFromRoot(root, source)
	byExported := map[string]string{}
	for _, edge := range edges {
		byExported[edge.exportedName] = edge.source
	}
	if byExported["A"] != "./mod" || byExported["C"] != "./mod" || byExported["*"] != "./star" {
		t.Errorf("reexport edges = %v, want A, C from ./mod and * from ./star", byExported)
	}
	if got := javaScriptTypeScriptStaticReexportsFromRoot(nil, source); len(got) != 0 {
		t.Errorf("nil root edges = %v, want empty", got)
	}
}

func TestReexportSourceCandidatesStayInRepo(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	fromPath := repoRoot + "/pkg/index.ts"
	got := javaScriptTypeScriptReexportSourceCandidates(repoRoot, fromPath, "./sibling")
	if len(got) == 0 {
		t.Fatal("relative re-export produced no candidates")
	}
	for _, candidate := range got {
		if len(candidate) < len(repoRoot) || candidate[:len(repoRoot)] != repoRoot {
			t.Errorf("candidate %q escapes repo root %q", candidate, repoRoot)
		}
	}
	if got := javaScriptTypeScriptReexportSourceCandidates(repoRoot, fromPath, "  "); len(got) != 0 {
		t.Errorf("blank source candidates = %v, want none", got)
	}
}

func TestPublicTypeReferencesFollowExportedTypes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export interface Config {
  svc: Service;
  count: number;
}
export interface Service {
  name: string;
}
`)
	defer closeFn()

	exported := map[string]struct{}{"Config": {}, "Service": {}}
	public := map[string]struct{}{"Config": {}}
	got := javaScriptTypeScriptPublicTypeReferences(root, source, public, exported)
	if _, ok := got["Service"]; !ok {
		t.Errorf("type references = %v, want Service", got)
	}
	if got := javaScriptTypeScriptPublicTypeReferences(root, source, nil, exported); len(got) != 0 {
		t.Errorf("empty public names = %v, want empty", got)
	}
	other := map[string]struct{}{"Missing": {}}
	if got := javaScriptTypeScriptPublicTypeReferences(root, source, other, exported); len(got) != 0 {
		t.Errorf("non-public filter = %v, want empty", got)
	}
}

func TestStaticRegistryMemberNamesNeedsExportedRegistry(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export const registry = { run: runHandler, idle: 1 };
function runHandler(): void {}
const local = { run: runHandler };
`)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	got := javaScriptTypeScriptStaticRegistryMemberNames(root, source, parents)
	if _, ok := got["runHandler"]; !ok {
		t.Errorf("registry members = %v, want runHandler", got)
	}
	if _, ok := got["idle"]; ok {
		t.Errorf("registry members = %v, want no idle (not a function)", got)
	}
	if got := javaScriptTypeScriptStaticRegistryMemberNames(nil, source, parents); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestFunctionNamesIncludesFunctionValuedDeclarators(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, "function top(): void {}\nconst handler = async (): Promise<void> => {};\nconst count = 1;\n")
	defer closeFn()

	got := javaScriptTypeScriptFunctionNames(root, source)
	for _, want := range []string{"top", "handler"} {
		if _, ok := got[want]; !ok {
			t.Errorf("function names = %v, want %q", got, want)
		}
	}
	if _, ok := got["count"]; ok {
		t.Errorf("function names = %v, want no count", got)
	}
}

func TestVariableDeclaratorHasFunctionValueShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, "const a = (): void => {};\nconst b = call();\nlet c;\n")
	defer closeFn()

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		results[shared.NodeText(name, source)] = javaScriptVariableDeclaratorHasFunctionValue(node)
	})
	for name, want := range map[string]bool{"a": true, "b": false, "c": false} {
		if results[name] != want {
			t.Errorf("function value for %s = %v, want %v (all: %v)", name, results[name], want, results)
		}
	}
	if javaScriptVariableDeclaratorHasFunctionValue(nil) {
		t.Error("nil node reported a function value")
	}
	if javaScriptVariableDeclaratorHasFunctionValue(root) {
		t.Error("program node reported a function value")
	}
}

func TestObjectLiteralIsExportedRegistryShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, "export const registry = { run: 1 };\nconst local = { run: 1 };\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	count := 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "object" && javaScriptObjectLiteralIsExportedRegistry(node, source, parents) {
			count++
		}
	})
	if count != 1 {
		t.Errorf("%d exported registries, want exactly 1", count)
	}
	if javaScriptObjectLiteralIsExportedRegistry(nil, source, parents) {
		t.Error("nil node reported as registry")
	}
}

func TestIsTypeScriptSourcePathTable(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"a.ts": true, "a.tsx": true, "a.mts": true, "a.cts": true,
		"A.TS": true, "a.js": false, "a.jsx": false, "a.d.ts": true, "a.css": false,
	} {
		if got := javaScriptIsTypeScriptSourcePath(path); got != want {
			t.Errorf("source path %s = %v, want %v", path, got, want)
		}
	}
}

func TestSameJavaScriptPathComparesCleaned(t *testing.T) {
	t.Parallel()

	if !sameJavaScriptPath("pkg//index.ts", "pkg/index.ts") {
		t.Error("cleaned-equal paths reported different")
	}
	if sameJavaScriptPath("a.ts", "b.ts") {
		t.Error("different paths reported same")
	}
	if sameJavaScriptPath("", "a.ts") || sameJavaScriptPath("a.ts", "") {
		t.Error("empty path reported same")
	}
}
