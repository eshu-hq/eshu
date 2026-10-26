// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPathDispatchesResolveToNamedDefinition(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	dispatches := PathDispatches()
	if len(dispatches) == 0 {
		t.Fatal("PathDispatches() is empty")
	}
	for _, dispatch := range dispatches {
		if len(dispatch.Examples) == 0 {
			t.Errorf("%v has no example path", dispatch.Patterns)
		}
		for _, example := range dispatch.Examples {
			definition, ok := registry.LookupByPath(example)
			if !ok {
				t.Errorf("LookupByPath(%q) found nothing, want %s", example, dispatch.ParserKey)
				continue
			}
			if definition.ParserKey != dispatch.ParserKey {
				t.Errorf("LookupByPath(%q) = %s, want %s", example, definition.ParserKey, dispatch.ParserKey)
			}
		}
	}
}

// literalByKeyLookups returns the sorted parser keys that the named function
// in src resolves through a literal r.byKey["<key>"] lookup, and the source
// positions of any r.byKey lookup whose index is not a string literal (those
// cannot be checked statically and are reported separately).
func literalByKeyLookups(t *testing.T, src, funcName string) (keys, nonLiteral []string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "registry.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatalf("function %s not found", funcName)
	}
	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		index, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		selector, ok := index.X.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "byKey" {
			return true
		}
		literal, ok := index.Index.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			nonLiteral = append(nonLiteral, fset.Position(index.Pos()).String())
			return true
		}
		key, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", literal.Value, err)
		}
		seen[key] = true
		return true
	})
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys, nonLiteral
}

// unlistedPathDispatchKeys returns the parser keys that lookupPath (the
// resolver LookupByPath wraps) in src binds through a literal byKey lookup but
// that dispatches does not list, and the keys dispatches lists that lookupPath
// no longer binds that way.
func unlistedPathDispatchKeys(t *testing.T, src string, dispatches []PathDispatch) (missing, stale []string) {
	t.Helper()

	bound, nonLiteral := literalByKeyLookups(t, src, "lookupPath")
	for _, position := range nonLiteral {
		t.Errorf("LookupByPath indexes byKey with a non-literal at %s; the PathDispatches guard cannot check it", position)
	}
	listed := map[string]bool{}
	for _, dispatch := range dispatches {
		listed[dispatch.ParserKey] = true
	}
	for _, key := range bound {
		if !listed[key] {
			missing = append(missing, key)
		}
	}
	for key := range listed {
		if !slices.Contains(bound, key) {
			stale = append(stale, key)
		}
	}
	slices.Sort(stale)
	return missing, stale
}

// TestPathDispatchesKeysMatchLookupByPath enforces that the parser keys listed
// in PathDispatches equal the keys LookupByPath binds through a literal
// r.byKey["<key>"] lookup. It checks keys only, not the path patterns: the
// example-path test covers the patterns it lists.
func TestPathDispatchesKeysMatchLookupByPath(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("registry.go")
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	missing, stale := unlistedPathDispatchKeys(t, string(raw), PathDispatches())
	for _, key := range missing {
		t.Errorf("LookupByPath binds %q by hardcoded dispatch but PathDispatches() does not list it", key)
	}
	for _, key := range stale {
		t.Errorf("PathDispatches() lists %q but LookupByPath no longer binds it by hardcoded dispatch", key)
	}
}

// pathDispatchSeed is a lookupPath with one listed branch (hcl) and one
// unlisted branch (seeded_unlisted) used to prove the guard reports a gap.
const pathDispatchSeed = `package parser

func (r Registry) lookupPath(path string) (Definition, bool) {
	if isListed(path) {
		d, ok := r.byKey["hcl"]
		return d, ok
	}
	if isSeeded(path) {
		d, ok := r.byKey["seeded_unlisted"]
		return d, ok
	}
	return Definition{}, false
}
`

func TestPathDispatchGuardReportsSeededUnlistedBinding(t *testing.T) {
	t.Parallel()

	missing, stale := unlistedPathDispatchKeys(t, pathDispatchSeed,
		[]PathDispatch{{ParserKey: "hcl"}})
	if !slices.Equal(missing, []string{"seeded_unlisted"}) {
		t.Fatalf("missing = %v, want [seeded_unlisted]", missing)
	}
	if len(stale) != 0 {
		t.Fatalf("stale = %v, want none", stale)
	}

	missing, stale = unlistedPathDispatchKeys(t, pathDispatchSeed,
		[]PathDispatch{{ParserKey: "hcl"}, {ParserKey: "seeded_unlisted"}, {ParserKey: "gone"}})
	if len(missing) != 0 || !slices.Equal(stale, []string{"gone"}) {
		t.Fatalf("missing = %v stale = %v, want none and [gone]", missing, stale)
	}
}

func TestAdmissionTableMarkdownListsPathDispatches(t *testing.T) {
	t.Parallel()

	table := AdmissionTableMarkdown(DefaultRegistry())
	for _, dispatch := range PathDispatches() {
		for _, pattern := range dispatch.Patterns {
			if !strings.Contains(table, "`"+pattern+"`") {
				t.Errorf("admission table misses path-dispatch pattern %q", pattern)
			}
		}
		if !strings.Contains(table, "`"+dispatch.ParserKey+"`") {
			t.Errorf("admission table misses path-dispatch parser key %q", dispatch.ParserKey)
		}
	}
	if !strings.Contains(table, "\n\nPath-dispatched bindings") {
		t.Error("admission table has no path-dispatch footnote after the table")
	}
	if !strings.Contains(table, "case-insensitively") {
		t.Error("path-dispatch footnote does not say paths match case-insensitively")
	}
}
