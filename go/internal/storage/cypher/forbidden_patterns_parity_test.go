// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// TestForbiddenPatternListsMatchLeaf pins the white-box gap probe's
// forbiddenRelationshipPatterns to the sweep/orphan leaf's
// forbiddenOrphanSweepPatterns. The two lists must stay behavior-identical:
// test helpers cannot be imported across the package split (the leaf imports
// this package, so an internal test here cannot import the leaf), and a
// future edit to either side must fail here instead of silently narrowing
// the relationship-existence predicate guard on one side.
func TestForbiddenPatternListsMatchLeaf(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed, cannot locate package directory")
	}
	pkgDir := filepath.Dir(thisFile)

	rootList := stringListFromVar(t, filepath.Join(pkgDir, "deployable_unit_gap_probe_test.go"), "forbiddenRelationshipPatterns")
	leafList := stringListFromVar(t, filepath.Join(pkgDir, "sweep", "orphan", "sweep_test.go"), "forbiddenOrphanSweepPatterns")

	sort.Strings(rootList)
	sort.Strings(leafList)

	if len(rootList) != len(leafList) {
		t.Fatalf("pattern list lengths diverge: root=%d (%q) leaf=%d (%q)",
			len(rootList), rootList, len(leafList), leafList)
	}
	for i := range rootList {
		if rootList[i] != leafList[i] {
			t.Fatalf("pattern lists diverge at sorted index %d: root=%q leaf=%q (full root=%q leaf=%q)",
				i, rootList[i], leafList[i], rootList, leafList)
		}
	}
}

func stringListFromVar(t *testing.T, file, name string) []string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile(%q) error = %v", file, err)
	}

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: %s is not a composite literal", file, name)
			}
			var out []string
			for _, elt := range lit.Elts {
				bl, ok := elt.(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s: %s contains a non-string element", file, name)
				}
				out = append(out, bl.Value[1:len(bl.Value)-1])
			}
			return out
		}
	}
	t.Fatalf("%s: var %s not found", file, name)
	return nil
}
