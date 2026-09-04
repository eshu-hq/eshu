// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// dirOf returns the directory of the file pos falls in, per s.fset. Every
// decl/call lookup in graph_read_error_capability_sweep_resolve_test.go
// resolves against the directory of the identifier or call expression
// actually being resolved -- never the directory of whatever code triggered
// the resolution -- which is what keeps a lookup scoped to the right package
// now that TestWriteGraphReadErrorCapabilitiesExistInMatrix's sweep walks
// go/internal/query recursively (#6060) instead of collecting every parsed
// package's declarations into one bare-identifier-keyed map. Before this,
// root and a leaf both declaring a symbol under the same bare name (root
// keeps a forwarder for many symbols a leaf owns, e.g. BoolVal, IntVal,
// BuildTruthEnvelope) meant a lookup could resolve to whichever package
// happened to be parsed last.
func (s *capabilitySweep) dirOf(pos token.Pos) string {
	return filepath.Dir(s.fset.Position(pos).Filename)
}

// TestCapabilitySweepResolvesDeclarationsFromTheirOwnDirectory is the #6060
// regression for directory-scoped resolution. It proves the hazard the
// former root-only decl-collection rule guarded against -- two directories
// declaring a same-named const resolving to whichever was parsed last -- is
// now prevented by directory scoping instead: it constructs two real,
// on-disk packages that each declare a const of the SAME bare name
// (sharedCap) with a DIFFERENT string value, parses both with the real
// capabilitySweep machinery (not a re-implementation of it), and asserts a
// reference inside each package's own function resolves to that package's
// own value, never the other's.
func TestCapabilitySweepResolvesDeclarationsFromTheirOwnDirectory(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "pkga")
	dirB := filepath.Join(root, "pkgb")
	if err := os.Mkdir(dirA, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dirA, err)
	}
	if err := os.Mkdir(dirB, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dirB, err)
	}

	const srcA = "package pkga\n\nconst sharedCap = \"value-from-a\"\n\nfunc UseA() string {\n\treturn sharedCap\n}\n"
	const srcB = "package pkgb\n\nconst sharedCap = \"value-from-b\"\n\nfunc UseB() string {\n\treturn sharedCap\n}\n"

	pathA := filepath.Join(dirA, "a.go")
	pathB := filepath.Join(dirB, "b.go")
	if err := os.WriteFile(pathA, []byte(srcA), 0o644); err != nil {
		t.Fatalf("write %s: %v", pathA, err)
	}
	if err := os.WriteFile(pathB, []byte(srcB), 0o644); err != nil {
		t.Fatalf("write %s: %v", pathB, err)
	}

	fset := token.NewFileSet()
	fileA, err := parser.ParseFile(fset, pathA, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", pathA, err)
	}
	fileB, err := parser.ParseFile(fset, pathB, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", pathB, err)
	}

	sweep := newCapabilitySweep(fset)
	sweep.collectDecls(fileA)
	sweep.collectDecls(fileB)

	fnA := findFuncDeclByName(t, fileA, "UseA")
	fnB := findFuncDeclByName(t, fileB, "UseB")
	identA := findReturnedIdent(t, fnA, "sharedCap")
	identB := findReturnedIdent(t, fnB, "sharedCap")

	valuesA, okA := sweep.resolveCapabilityArg(identA, fnA, map[string]bool{})
	if !okA || len(valuesA) != 1 || valuesA[0] != "value-from-a" {
		t.Fatalf("resolveCapabilityArg(sharedCap in pkga) = %v, %v; want ([value-from-a], true)", valuesA, okA)
	}

	valuesB, okB := sweep.resolveCapabilityArg(identB, fnB, map[string]bool{})
	if !okB || len(valuesB) != 1 || valuesB[0] != "value-from-b" {
		t.Fatalf("resolveCapabilityArg(sharedCap in pkgb) = %v, %v; want ([value-from-b], true)", valuesB, okB)
	}
}

// findFuncDeclByName returns the *ast.FuncDecl named name in file, failing
// the test if it is not present.
func findFuncDeclByName(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("no func decl named %s in %s", name, file.Name.Name)
	return nil
}

// findReturnedIdent returns the first *ast.Ident named name appearing as a
// single-value return result inside fn, failing the test if none is found.
func findReturnedIdent(t *testing.T, fn *ast.FuncDecl, name string) *ast.Ident {
	t.Helper()
	var found *ast.Ident
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if ident, ok := ret.Results[0].(*ast.Ident); ok && ident.Name == name {
			found = ident
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no return of identifier %s in %s", name, fn.Name.Name)
	}
	return found
}
