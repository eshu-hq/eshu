// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestNamedImportsByLocalNameSkipsDefaultAndStar(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { A, B as C } from "./mod";
import Def from "./def";
import * as ns from "./ns";
import "./side";
`)
	defer closeFn()

	got := javaScriptTypeScriptNamedImportsByLocalName(root, source)
	if binding, ok := got["A"]; !ok || binding.importedName != "A" || binding.source != "./mod" {
		t.Errorf("binding A = %+v, want imported A from ./mod (all: %v)", binding, got)
	}
	if binding, ok := got["C"]; !ok || binding.importedName != "B" || binding.source != "./mod" {
		t.Errorf("binding C = %+v, want imported B from ./mod (all: %v)", binding, got)
	}
	for _, local := range []string{"Def", "ns"} {
		if _, ok := got[local]; ok {
			t.Errorf("binding %s present, want default and star imports skipped (all: %v)", local, got)
		}
	}
	if got := javaScriptTypeScriptNamedImportsByLocalName(nil, source); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestDeclarationMentionedNamesMatchesImportLocals(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { Service, Unused } from "./mod";
export interface Config {
  svc: Service;
  count: number;
}
`)
	defer closeFn()

	var decl *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if decl == nil && node.Kind() == "interface_declaration" {
			decl = node
		}
	})
	imports := javaScriptTypeScriptNamedImportsByLocalName(root, source)
	got := javaScriptTypeScriptDeclarationMentionedNames(decl, source, imports)
	if _, ok := got["Service"]; !ok {
		t.Errorf("mentioned = %v, want Service", got)
	}
	if _, ok := got["Unused"]; ok {
		t.Errorf("mentioned = %v, want no Unused", got)
	}
	if got := javaScriptTypeScriptDeclarationMentionedNames(nil, source, imports); len(got) != 0 {
		t.Errorf("nil declaration = %v, want empty", got)
	}
}

func TestIsPublicDeclarationKindTable(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseTypeScriptRootForTest(t, `interface I {}
type T = string;
class C {}
abstract class A {}
enum E { X }
function f(): void {}
const v = 1;
`)
	defer closeFn()

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "interface_declaration", "type_alias_declaration", "class_declaration",
			"abstract_class_declaration", "enum_declaration", "function_declaration",
			"variable_declarator":
			results[node.Kind()] = javaScriptTypeScriptIsPublicDeclarationKind(node)
		}
	})
	for kind, want := range map[string]bool{
		"interface_declaration": true, "type_alias_declaration": true,
		"class_declaration": true, "abstract_class_declaration": true,
		"enum_declaration": true, "function_declaration": false,
		"variable_declarator": false,
	} {
		if results[kind] != want {
			t.Errorf("public kind %s = %v, want %v (all: %v)", kind, results[kind], want, results)
		}
	}
	if javaScriptTypeScriptIsPublicDeclarationKind(nil) {
		t.Error("nil node reported public")
	}
}

func TestStringOrTable(t *testing.T) {
	t.Parallel()

	if got := stringOr("x"); got != "x" {
		t.Errorf("stringOr(string) = %q, want x", got)
	}
	if got := stringOr(42); got != "" {
		t.Errorf("stringOr(int) = %q, want empty", got)
	}
	if got := stringOr(nil); got != "" {
		t.Errorf("stringOr(nil) = %q, want empty", got)
	}
}

func TestImportedBindingsForTargetResolvesBySource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	target := filepath.Join(repoRoot, "pkg", "target.ts")
	from := filepath.Join(repoRoot, "pkg", "index.ts")
	facts := typeScriptPublicSurfaceNodeFacts{
		namedImportsByLocalName: map[string]javaScriptTypeScriptImportedBinding{
			"Widget": {importedName: "Widget", source: "./target"},
			"Gadget": {importedName: "Gadget", source: "./elsewhere"},
		},
		ok: true,
	}
	exported := map[string]struct{}{"Widget": {}, "Gadget": {}}

	got := javaScriptTypeScriptImportedBindingsForTarget(repoRoot, from, target, facts, exported)
	if binding, ok := got["Widget"]; !ok || binding.importedName != "Widget" {
		t.Errorf("target bindings = %v, want Widget resolved to the target", got)
	}
	if _, ok := got["Gadget"]; ok {
		t.Errorf("target bindings = %v, want no Gadget (different source)", got)
	}
	empty := typeScriptPublicSurfaceNodeFacts{}
	if got := javaScriptTypeScriptImportedBindingsForTarget(repoRoot, from, target, empty, exported); len(got) != 0 {
		t.Errorf("empty facts = %v, want none", got)
	}
	unexported := map[string]struct{}{"Other": {}}
	if got := javaScriptTypeScriptImportedBindingsForTarget(repoRoot, from, target, facts, unexported); len(got) != 0 {
		t.Errorf("unexported filter = %v, want none", got)
	}
}

func TestImportedTypeReferencesFromPublicDeclarationsFilters(t *testing.T) {
	t.Parallel()

	facts := typeScriptPublicSurfaceNodeFacts{
		declarationMentions: map[string]map[string]struct{}{
			"Config": {"Service": {}, "Helper": {}},
		},
	}
	bindings := map[string]javaScriptTypeScriptImportedBinding{
		"Service": {importedName: "Service", source: "./ svc"},
		"Helper":  {importedName: "Helper", source: "./help"},
	}

	star := javaScriptTypeScriptSurfaceWalkItem{path: "p", star: true}
	got := javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(star, facts, bindings)
	if len(got) != 2 {
		t.Errorf("star references = %v, want both mentions", got)
	}

	named := javaScriptTypeScriptSurfaceWalkItem{path: "p", names: map[string]struct{}{"Config": {}}}
	got = javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(named, facts, bindings)
	if len(got) != 2 {
		t.Errorf("named references = %v, want both mentions for Config", got)
	}
	other := javaScriptTypeScriptSurfaceWalkItem{path: "p", names: map[string]struct{}{"Missing": {}}}
	if got := javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(other, facts, bindings); len(got) != 0 {
		t.Errorf("unmatched declaration = %v, want empty", got)
	}
	if got := javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(star, facts, nil); len(got) != 0 {
		t.Errorf("empty bindings = %v, want empty", got)
	}
}

func TestPublicDeclarationMentionsByNameUnionsMerges(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { A, B } from "./mod";
export interface Widget {
  a: A;
}
export interface Widget {
  b: B;
}
export function hidden(a: A): void {}
interface Private {
  b: B;
}
`)
	defer closeFn()

	got := javaScriptTypeScriptPublicDeclarationMentionsByName(root, source)
	mentioned, ok := got["Widget"]
	if !ok {
		t.Fatalf("mentions = %v, want Widget", got)
	}
	if _, ok := mentioned["A"]; !ok {
		t.Errorf("Widget mentions = %v, want A from the first block", mentioned)
	}
	if _, ok := mentioned["B"]; !ok {
		t.Errorf("Widget mentions = %v, want B unioned from the merged block", mentioned)
	}
	if _, ok := got["Private"]; ok {
		t.Errorf("mentions = %v, want no unexported Private", got)
	}
	if got := javaScriptTypeScriptPublicDeclarationMentionsByName(nil, source); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestCacheEncodeRoundTrip(t *testing.T) {
	t.Parallel()

	key := packageSurfaceCacheEncode("/pkg", "/pkg/index.ts")
	if got := packageSurfaceHookPathFromKey(key); got != "/pkg/index.ts" {
		t.Errorf("hook path = %q, want the file portion", got)
	}
	if got := packageSurfaceHookPathFromKey("bare"); got != "bare" {
		t.Errorf("separator-less key = %q, want passthrough", got)
	}
}

func TestComputePackageSurfaceFactsReadsSibling(t *testing.T) {
	// Serial: exercises the shared package-surface computation (not the
	// hook) with distinct temp paths per case.
	path := filepath.Join(t.TempDir(), "index.ts")
	stub := newMapSiblingSource(t, map[string]string{
		path: "export { A } from \"./mod\";\nimport { B } from \"./b\";\nexport interface Cfg { b: B; }\n",
	})

	facts := computePackageSurfaceFacts(path, stub)
	if !facts.ok {
		t.Fatal("sibling-backed facts not ok")
	}
	if len(facts.reexports) == 0 {
		t.Error("no re-export edges extracted")
	}
	if _, ok := facts.namedImportsByLocalName["B"]; !ok {
		t.Errorf("named imports = %v, want B", facts.namedImportsByLocalName)
	}
	if _, ok := facts.declarationMentions["Cfg"]; !ok {
		t.Errorf("declaration mentions = %v, want Cfg", facts.declarationMentions)
	}

	missing := computePackageSurfaceFacts(path, nil)
	if missing.ok {
		t.Error("nil sibling facts ok, want not-ok")
	}
	absent := computePackageSurfaceFacts(filepath.Join(t.TempDir(), "gone.ts"), stub)
	if absent.ok {
		t.Error("absent-path facts ok, want not-ok")
	}
}

func TestPackageSurfaceFactsCachesByStat(t *testing.T) {
	// Serial: same global cache as production; the temp path is unique to
	// this test so no other test can observe or pollute the entry.
	dir := t.TempDir()
	path := filepath.Join(dir, "index.ts")
	content := "export const version = 1;\n"
	stub := newMapSiblingSource(t, map[string]string{path: content})
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	first := packageSurfaceFacts(dir, path, stub)
	if !first.ok {
		t.Fatal("file-backed facts not ok")
	}
	second := packageSurfaceFacts(dir, path, stub)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("cached facts differ: %#v vs %#v", first, second)
	}

	missing := packageSurfaceFacts(dir, filepath.Join(dir, "gone.ts"), nil)
	if missing.ok {
		t.Error("missing-path facts ok, want not-ok")
	}
}

func TestRegisteredRootKindsGatesOnExpressImport(t *testing.T) {
	t.Parallel()

	// No express import: the stub is never consulted and the result stays
	// empty even though a server symbol exists.
	root, source, closeFn := parseJavaScriptSourceForTest(t, "const app = make();\napp.get(\"/x\", handler);\nfunction handler() {}\n")
	defer closeFn()

	if got := RegisteredDeadCodeRootKinds(root, source, expressStub()); len(got) != 0 {
		t.Errorf("no-import registrations = %v, want empty", got)
	}
	if got := RegisteredDeadCodeRootKinds(root, source, nil); len(got) != 0 {
		t.Errorf("nil framework registrations = %v, want empty", got)
	}
	if got := RegisteredDeadCodeRootKinds(nil, source, expressStub()); len(got) != 0 {
		t.Errorf("nil root registrations = %v, want empty", got)
	}
}

func TestExpressServerSymbolsReadsContract(t *testing.T) {
	t.Parallel()

	if got := ExpressServerSymbols(map[string]any{"server_symbols": []string{"app"}}); !reflect.DeepEqual(got, []string{"app"}) {
		t.Errorf("server symbols = %v, want [app]", got)
	}
	if got := ExpressServerSymbols(map[string]any{"server_symbols": "app"}); got != nil {
		t.Errorf(" mistyped symbols = %v, want nil", got)
	}
	if got := ExpressServerSymbols(nil); got != nil {
		t.Errorf("empty express = %v, want nil", got)
	}
}

func TestRootKindsCarriesEvidenceMaps(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "export function run() {}\nfunction main() {}\nfunction helper() {}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	funcs := map[string]*tree_sitter.Node{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "function_declaration" {
			name := node.ChildByFieldName("name")
			funcs[string(source[name.StartByte():name.EndByte()])] = node
		}
	})

	// Registered kinds flow through the evidence map keyed by name.
	evidence := Evidence{
		registeredRootKinds: map[string][]string{"main": {"javascript.express_route_registration"}},
		FileRootKinds:       []string{"javascript.node_package_entrypoint"},
		Parents:             parents,
	}
	got := RootKinds("svc/server.js", funcs["main"], "main", source, evidence)
	if !containsString(got, "javascript.express_route_registration") || !containsString(got, "javascript.node_package_entrypoint") {
		t.Errorf("main root kinds = %v, want registered and entrypoint kinds", got)
	}

	// Unknown names with no evidence stay unreachable-only.
	evidence = Evidence{Parents: parents}
	if got := RootKinds("svc/server.js", funcs["helper"], "helper", source, evidence); len(got) != 0 {
		t.Errorf("helper root kinds = %v, want empty", got)
	}

	// A hapi handler file marks exported functions: run is ES-exported.
	evidence = Evidence{hapiHandlerFile: true, Parents: parents}
	if got := RootKinds("svc/handlers/a.js", funcs["run"], "run", source, evidence); !containsString(got, "javascript.hapi_handler_export") {
		t.Errorf("handler root kinds = %v, want the hapi handler export", got)
	}

	// TypeScript surface kinds append verbatim.
	evidence = Evidence{typeScriptRootKinds: map[string][]string{"run": {"typescript.public_api_export"}}, Parents: parents}
	if got := RootKinds("pkg/a.ts", funcs["run"], "run", source, evidence); !containsString(got, "typescript.public_api_export") {
		t.Errorf("surface root kinds = %v, want the TS kind", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
