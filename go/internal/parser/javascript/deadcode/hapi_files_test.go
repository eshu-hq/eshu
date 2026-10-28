// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// mapSiblingSource serves in-memory sources by exact path for seam tests.
// The grammar follows the file extension so TypeScript siblings parse as
// TypeScript. Parsed trees are retained so returned nodes stay valid.
type mapSiblingSource struct {
	sources map[string]string
	trees   []*tree_sitter.Tree
	js      *tree_sitter.Parser
	ts      *tree_sitter.Parser
}

func newMapSiblingSource(t *testing.T, sources map[string]string) *mapSiblingSource {
	t.Helper()
	newParser := func(lang *tree_sitter.Language) *tree_sitter.Parser {
		t.Helper()
		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(lang); err != nil {
			parser.Close()
			t.Fatalf("SetLanguage() error = %v, want nil", err)
		}
		t.Cleanup(parser.Close)
		return parser
	}
	return &mapSiblingSource{
		sources: sources,
		js:      newParser(tree_sitter.NewLanguage(tree_sitter_javascript.Language())),
		ts:      newParser(tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())),
	}
}

func (m *mapSiblingSource) RootForFile(path string) (*tree_sitter.Node, []byte, bool) {
	source, ok := m.sources[path]
	if !ok {
		return nil, nil, false
	}
	parser := m.js
	lower := strings.ToLower(path)
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(lower, ext) {
			parser = m.ts
			break
		}
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		return nil, nil, false
	}
	m.trees = append(m.trees, tree)
	return tree.RootNode(), bytes, true
}

const hapiSpecPluginSource = `const path = require("path");
module.exports = {
  openapi: true,
  handlers: path.join(__dirname, "../../../svc/handlers"),
};
`

func TestIsHapiHandlerFileMatchesSpecDeclaredDirs(t *testing.T) {
	// The cache-free seam path resolves through the filesystem-bounded
	// package root, so repoRoot is a temp dir: hermetic by construction.
	repoRoot := t.TempDir()
	specPath := filepath.Join(repoRoot, "server", "init", "plugins", "spec.js")
	stub := newMapSiblingSource(t, map[string]string{specPath: hapiSpecPluginSource})

	handlerPath := filepath.Join(repoRoot, "svc", "handlers", "route.js")
	if !javaScriptIsHapiHandlerFile(repoRoot, handlerPath, stub) {
		t.Error("spec-declared handler file not recognized")
	}
	outsidePath := filepath.Join(repoRoot, "svc", "other", "route.js")
	if javaScriptIsHapiHandlerFile(repoRoot, outsidePath, stub) {
		t.Error("file outside the handlers dir recognized as handler")
	}
	if javaScriptIsHapiHandlerFile(repoRoot, handlerPath, nil) {
		t.Error("nil sibling source recognized a handler file")
	}
	if javaScriptIsHapiHandlerFile("", handlerPath, stub) {
		t.Error("empty repo root recognized a handler file")
	}
	if javaScriptIsHapiHandlerFile(repoRoot, "", stub) {
		t.Error("empty path recognized as a handler file")
	}
}

func TestHapiHandlerSpecDirsFindsHandlersPairs(t *testing.T) {
	root, source, closeFn := parseJavaScriptSourceForTest(t, `const cfg = {
  handlers: path.resolve(__dirname, "handlers"),
  other: path.join(__dirname, "elsewhere"),
  broken: path.resolve("nowhere"),
};`)
	defer closeFn()

	got := javaScriptHapiHandlerSpecDirs(root, source)
	if len(got) != 1 || got[0] != "handlers" {
		t.Errorf("spec dirs = %v, want [handlers]", got)
	}
	if got := javaScriptHapiHandlerSpecDirs(nil, nil); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestDirnamePathArgumentValidatesCallShape(t *testing.T) {
	root, source, closeFn := parseJavaScriptSourceForTest(t, `const a = path.resolve(__dirname, "handlers");
const b = path.join(__dirname, "more");
const c = path.resolve("nowhere");
const d = path.del(__dirname, "gone");
const e = notpath.join(__dirname, "x");
const f = path.join(__dirname);
`)
	defer closeFn()

	results := map[string]struct {
		value string
		ok    bool
	}{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value, ok := javaScriptDirnamePathArgument(node.ChildByFieldName("value"), source)
		results[string(source[name.StartByte():name.EndByte()])] = struct {
			value string
			ok    bool
		}{value, ok}
	})

	if results["a"] != (struct {
		value string
		ok    bool
	}{"handlers", true}) {
		t.Errorf("resolve case = %+v, want handlers/true", results["a"])
	}
	if results["b"] != (struct {
		value string
		ok    bool
	}{"more", true}) {
		t.Errorf("join case = %+v, want more/true", results["b"])
	}
	for _, name := range []string{"c", "d", "e", "f"} {
		if results[name].ok {
			t.Errorf("%s = %+v, want ok=false", name, results[name])
		}
	}
	if _, ok := javaScriptDirnamePathArgument(nil, source); ok {
		t.Error("nil node reported ok")
	}
}

func TestLooksLikeHapiSpecsPluginMatchesOpenAPI(t *testing.T) {
	t.Parallel()

	if !javaScriptLooksLikeHapiSpecsPlugin("module.exports = { OpenAPI: true };") {
		t.Error("openapi marker (any case) not recognized")
	}
	if javaScriptLooksLikeHapiSpecsPlugin("module.exports = { routes: [] };") {
		t.Error("plain plugin recognized as specs plugin")
	}
}

func TestIsHapiProxyCallbackNeedsProxyCall(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, `server.proxy({ mapUri: (req) => "x" });
server.other({ onResponse: (req) => "y" });
server.proxy({ other: 1 });
`)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	count := 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "pair" {
			key := node.ChildByFieldName("key")
			if javaScriptIsHapiProxyCallback(node, string(source[key.StartByte():key.EndByte()]), source, parents) {
				count++
			}
		}
	})
	if count != 1 {
		t.Errorf("%d proxy callbacks, want exactly 1 (mapUri inside server.proxy)", count)
	}
	if javaScriptIsHapiProxyCallback(nil, "mapUri", source, parents) {
		t.Error("nil node reported as proxy callback")
	}
}

func TestIsHapiProxyCallbackNameTable(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{"mapUri": true, "onResponse": true, "handler": false, "": false, " mapUri ": true} {
		if got := javaScriptIsHapiProxyCallbackName(name); got != want {
			t.Errorf("proxy callback name %q = %v, want %v", name, got, want)
		}
	}
}

func TestIsNextJSAppModuleAndExport(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"app/page.tsx": true, "app/layout.js": true, "ui/template.ts": true,
		"app/error.jsx": true, "app/loading.mts": false, "app/not-found.tsx": true,
		"app/route.ts": false, "app/page.css": false,
	} {
		if got := javaScriptIsNextJSAppModule(path); got != want {
			t.Errorf("app module %s = %v, want %v", path, got, want)
		}
	}

	root, _, closeFn := parseJavaScriptSourceForTest(t, "export default function Page() {}\nfunction Hidden() {}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	exported, hidden := false, false
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		if javaScriptIsNextJSAppExport("app/page.js", node, parents) {
			name := node.ChildByFieldName("name")
			if name != nil {
				exported = true
			}
		}
		if !javaScriptIsNextJSAppExport("app/page.js", node, parents) {
			hidden = true
		}
	})
	if !exported || !hidden {
		t.Errorf("exported=%v hidden=%v, want the export recognized and the plain function not", exported, hidden)
	}
	if javaScriptIsNextJSAppExport("app/other.js", root, parents) {
		t.Error("non-app module reported an app export")
	}
}

func TestIsNodeMigrationExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "export async function up() {}\nexport async function down() {}\nexport async function seed() {}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		nameText := ""
		if name != nil {
			nameText = string(source[name.StartByte():name.EndByte()])
		}
		results[nameText] = javaScriptIsNodeMigrationExport("db/migrations/001.js", node, nameText, parents)
	})
	if !results["up"] || !results["down"] {
		t.Errorf("migration results = %v, want up and down true", results)
	}
	if results["seed"] {
		t.Errorf("migration results = %v, want seed false", results)
	}

	var upNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if upNode == nil && node.Kind() == "function_declaration" {
			upNode = node
		}
	})
	if javaScriptIsNodeMigrationExport("db/other/001.js", upNode, "up", parents) {
		t.Error("non-migrations path reported a migration export")
	}
	for path, want := range map[string]bool{"migrations/1.js": true, "db/migrations/1.js": true, "db/other/1.js": false} {
		if got := javaScriptIsNodeMigrationFile(path); got != want {
			t.Errorf("migration file %s = %v, want %v", path, got, want)
		}
	}
}

func TestIsTypeScriptModuleContractExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "export const RULE_NAME = \"no-eval\";\nexport function validate(node) { return true; }\nexport function helper() { return 1; }\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		nameText := ""
		if name != nil {
			nameText = string(source[name.StartByte():name.EndByte()])
		}
		results[nameText] = javaScriptIsTypeScriptModuleContractExport(node, nameText, source, parents)
	})
	if !results["validate"] {
		t.Errorf("contract results = %v, want validate true", results)
	}
	if results["helper"] {
		t.Errorf("contract results = %v, want helper false", results)
	}
	if javaScriptIsTypeScriptModuleContractExport(nil, "validate", source, parents) {
		t.Error("nil node reported as contract export")
	}
}

func TestProgramNodeAndExportedConst(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "export const RULE_NAME = \"x\";\nconst LOCAL = 1;\nfunction f() {}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	var funcNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_declaration" {
			funcNode = node
		}
	})
	program := javaScriptProgramNode(funcNode, parents)
	if program == nil || program.Kind() != "program" {
		t.Errorf("program node = %v, want the program", program)
	}
	if !javaScriptProgramHasExportedConst(program, "RULE_NAME", source, parents) {
		t.Error("exported RULE_NAME const not found")
	}
	if javaScriptProgramHasExportedConst(program, "LOCAL", source, parents) {
		t.Error("unexported const reported exported")
	}
	if javaScriptProgramHasExportedConst(nil, "RULE_NAME", source, parents) {
		t.Error("nil program reported a const")
	}
	if javaScriptProgramHasExportedConst(program, "  ", source, parents) {
		t.Error("blank name reported a const")
	}
	if got := javaScriptProgramNode(nil, parents); got != nil {
		t.Errorf("nil node program = %v, want nil", got)
	}
}
