// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func importStatements(t *testing.T, root *tree_sitter.Node) []*tree_sitter.Node {
	t.Helper()
	var stmts []*tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "import_statement" {
			stmts = append(stmts, node)
		}
	})
	return stmts
}

func TestImportEntriesDefaultNamespaceAndNamed(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "import Def from \"./def\";\nimport * as ns from \"./ns\";\nimport { A, B as C } from \"./mod\";\n")
	defer closeFn()
	stmts := importStatements(t, root)
	if len(stmts) != 3 {
		t.Fatalf("found %d import statements, want 3", len(stmts))
	}

	def := ImportEntries(stmts[0], source, "javascript")
	if len(def) != 1 || def[0]["name"] != "default" || def[0]["alias"] != "Def" || def[0]["source"] != "./def" {
		t.Errorf("default import = %v, want one default/Def row", def)
	}
	ns := ImportEntries(stmts[1], source, "javascript")
	if len(ns) != 1 || ns[0]["name"] != "*" || ns[0]["alias"] != "ns" {
		t.Errorf("namespace import = %v, want one star/ns row", ns)
	}
	named := ImportEntries(stmts[2], source, "javascript")
	byName := map[string]any{}
	for _, item := range named {
		name, _ := item["name"].(string)
		byName[name] = item["alias"]
	}
	if byName["A"] != "" || byName["B"] != "C" {
		t.Errorf("named imports = %v, want A unaliased and B as C", byName)
	}
	for _, items := range [][]map[string]any{def, ns, named} {
		for _, item := range items {
			if item["lang"] != "javascript" {
				t.Errorf("entry %v lost its lang", item)
			}
		}
	}
}

func TestImportEntriesSideEffectNamesModule(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "import \"./setup\";\n")
	defer closeFn()
	stmts := importStatements(t, root)
	if len(stmts) != 1 {
		t.Fatalf("found %d import statements, want 1", len(stmts))
	}
	got := ImportEntries(stmts[0], source, "javascript")
	if len(got) != 1 || got[0]["name"] != "./setup" || got[0]["source"] != "./setup" {
		t.Errorf("side-effect import = %v, want one module-naming row", got)
	}
}

func TestImportEntriesTypeModifierFlagsRows(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "import type { A } from \"./mod\";\nimport { B } from \"./other\";\n")
	defer closeFn()
	stmts := importStatements(t, root)
	if len(stmts) != 2 {
		t.Fatalf("found %d import statements, want 2", len(stmts))
	}
	typed := ImportEntries(stmts[0], source, "typescript")
	if len(typed) != 1 || typed[0]["name"] != "A" {
		t.Fatalf("type import = %v, want one row for A", typed)
	}
	if _, ok := typed[0][shared.ImportFlagTypeOnly]; !ok {
		t.Errorf("type import row = %v, want the type-only flag", typed[0])
	}
	plain := ImportEntries(stmts[1], source, "typescript")
	if _, ok := plain[0][shared.ImportFlagTypeOnly]; ok {
		t.Errorf("plain import row = %v, want no type-only flag", plain[0])
	}
}

func TestNamespaceImportAliasReadsNameField(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "import * as ns from \"./ns\";\nimport Def from \"./def\";\n")
	defer closeFn()

	var nsNode, defNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "namespace_import":
			if nsNode == nil {
				nsNode = node
			}
		case "import_statement":
			_ = node
		}
	})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if defNode == nil && node.Kind() == "identifier" && shared.NodeText(node, source) == "Def" {
			defNode = node
		}
	})
	if got := NamespaceImportAlias(nsNode, source); got != "ns" {
		t.Errorf("namespace alias = %q, want ns", got)
	}
	if got := NamespaceImportAlias(defNode, source); got != "" {
		t.Errorf("non-namespace alias = %q, want empty", got)
	}
	if got := NamespaceImportAlias(nil, source); got != "" {
		t.Errorf("nil node alias = %q, want empty", got)
	}
}

func TestHasTypeModifierReadsTokenChildren(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseTSRootForTest(t, "import type { A } from \"./mod\";\nimport { B } from \"./other\";\n")
	defer closeFn()
	stmts := importStatements(t, root)
	if !hasTypeModifier(stmts[0]) {
		t.Error("type import not flagged")
	}
	if hasTypeModifier(stmts[1]) {
		t.Error("plain import flagged as type")
	}
	if hasTypeModifier(nil) {
		t.Error("nil node flagged as type")
	}
}
