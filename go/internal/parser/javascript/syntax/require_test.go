// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestRequireImportEntriesPlainAndMember(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const app = require(\"./app\");\nconst handler = require(\"./srv\").handler;\nconst same = require(\"./srv\").same;\n")
	defer closeFn()

	// Same-name member entries carry no alias key, so index by name and
	// keep the alias separately for the assertions below.
	entries := map[string]map[string]any{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		for _, entry := range RequireImportEntries(node, source, "javascript") {
			name, _ := entry["name"].(string)
			alias, _ := entry["alias"].(string)
			key := alias
			if key == "" {
				key = "name:" + name
			}
			entries[key] = entry
			entries[key]["_name"] = name
		}
	})
	entries["handler"] = entries["name:handler"]
	entries["same"] = entries["name:same"]

	plain, ok := entries["app"]
	if !ok {
		t.Fatalf("entries = %v, want app", entries)
	}
	if plain["_name"] != "*" || plain["source"] != "./app" || plain["import_type"] != "require" || plain["lang"] != "javascript" {
		t.Errorf("plain require entry = %v, want star import of ./app", plain)
	}
	handler, ok := entries["handler"]
	if !ok {
		t.Fatalf("entries = %v, want handler", entries)
	}
	if handler["_name"] != "handler" || handler["source"] != "./srv" {
		t.Errorf("member require entry = %v, want handler from ./srv", handler)
	}
	if _, ok := handler["alias"]; ok {
		t.Errorf("same-name member entry carries an alias: %v", handler)
	}
	if entries["same"]["_name"] != "same" {
		t.Errorf("entries = %v, want same", entries)
	}
}

func TestRequireImportEntriesDestructured(t *testing.T) {
	t.Parallel()

	// The rest element without a preceding space strips its dots; with a
	// space the TrimPrefix misses (prefix order) and the dots stay. Both
	// are pinned as the current contract.
	root, source, closeFn := parseRootForTest(t, "const { a, b: renamed,...tight } = require(\"./mod\");\nconst { ...loose } = require(\"./mod\");\nconst [first] = require(\"./arr\");\n")
	defer closeFn()

	byAlias := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		for _, entry := range RequireImportEntries(node, source, "javascript") {
			name, _ := entry["name"].(string)
			alias, _ := entry["alias"].(string)
			if alias == "" {
				alias = name
			}
			byAlias[alias] = name
		}
	})
	if byAlias["a"] != "a" || byAlias["renamed"] != "b" {
		t.Errorf("destructured basics = %v, want a->a renamed->b", byAlias)
	}
	if byAlias["tight"] != "tight" {
		t.Errorf("tight rest = %q, want dots stripped (all: %v)", byAlias["tight"], byAlias)
	}
	if byAlias["...loose"] != "...loose" {
		t.Errorf("loose rest = %v, want dots kept (all: %v)", byAlias, byAlias)
	}
	if _, ok := byAlias["first"]; ok {
		t.Errorf("array pattern destructured into entries: %v", byAlias)
	}
}

func TestRequireImportEntriesRejects(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const x = compute();\nconst y = require(dynamic);\nconst z = require(\"a\", \"b\");\nconst t = require(`./${name}`);\n")
	defer closeFn()

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		if got := RequireImportEntries(node, source, "javascript"); len(got) != 0 {
			t.Errorf("unresolvable require produced entries: %v", got)
		}
	})
	if got := RequireImportEntries(nil, source, "javascript"); len(got) != 0 {
		t.Errorf("nil node = %v, want nil", got)
	}
	if got := RequireImportEntries(root, source, "javascript"); len(got) != 0 {
		t.Errorf("program node = %v, want nil", got)
	}
}

func TestRequireModuleSourceShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const a = require(\"./ok\");\nconst b = require('./single');\nconst c = require(name);\n")
	defer closeFn()

	results := map[string]struct {
		module string
		ok     bool
	}{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		module, ok := RequireModuleSource(node.ChildByFieldName("value"), source)
		results[shared.NodeText(name, source)] = struct {
			module string
			ok     bool
		}{module, ok}
	})
	if results["a"] != (struct {
		module string
		ok     bool
	}{"./ok", true}) {
		t.Errorf("double-quoted = %+v, want ok", results["a"])
	}
	if results["b"] != (struct {
		module string
		ok     bool
	}{"./single", true}) {
		t.Errorf("single-quoted = %+v, want ok", results["b"])
	}
	if results["c"].ok {
		t.Errorf("computed = %+v, want not-ok", results["c"])
	}
	if _, ok := RequireModuleSource(nil, source); ok {
		t.Error("nil node reported ok")
	}
}
