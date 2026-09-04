// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"
)

// Package-qualified capability resolution lives here, split out of
// graph_read_error_capability_sweep_resolve_test.go once that file crossed
// the repo's 500-line cap: resolveQualifiedConst plus the import tracking it
// depends on, and every regression test that exercises a pkg.Name selector
// (the #6060 leaf shape, where staying root handlers pass
// advisory.AdvisoryEvidenceCapability through
// queryselector.ResolveForRequestWithAccess).

// collectFileImports records every import of file keyed by the local name
// the file uses for it, so resolveQualifiedConst can bind a pkg.Name
// selector through the consuming file's own import list.
func (s *capabilitySweep) collectFileImports(file *ast.File) {
	filename := s.fset.Position(file.Package).Filename
	for _, imp := range file.Imports {
		if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			continue
		}
		raw, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if s.fileImports[filename] == nil {
			s.fileImports[filename] = map[string]string{}
		}
		s.fileImports[filename][importLocalName(imp, raw)] = raw
	}
}

// importLocalName reports the name a file uses for an imported package
// path: the explicit rename when present, else the path's final element.
func importLocalName(imp *ast.ImportSpec, raw string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	return path.Base(raw)
}

// resolveQualifiedConst resolves pkg.Name to the string literal the named
// constant declares in the swept package the consuming file imports under
// that name. Two gates, both fail-closed:
//
//  1. Import binding: the consuming file must import a package under the
//     selector receiver's spelling. A same-spelled local variable or struct
//     field has no import entry and does not resolve, instead of resolving
//     to the swept package's literal.
//  2. Directory binding + unanimity: among swept directories declaring that
//     package name, only ones whose final path element matches the imported
//     path's (the imported package, not a same-named one) contribute, and
//     they must all agree on the value.
//
// Accepted residual: a file that both imports the package and shadows its
// name with a same-fielded local still resolves to the literal. No such
// shadowing exists in the swept tree, and the sweep guards registration,
// not runtime values (the compiled call uses the real value either way).
func (s *capabilitySweep) resolveQualifiedConst(e *ast.SelectorExpr) ([]string, bool) {
	pkgIdent, ok := e.X.(*ast.Ident)
	if !ok || e.Sel == nil {
		return nil, false
	}
	consumer := s.fset.Position(e.Pos()).Filename
	importPath, ok := s.fileImports[consumer][pkgIdent.Name]
	if !ok {
		return nil, false
	}
	wantBase := path.Base(importPath)
	var values []string
	for dir, pkgName := range s.packageNames {
		if pkgName != pkgIdent.Name {
			continue
		}
		if filepath.Base(dir) != wantBase {
			continue
		}
		lit, ok := s.constStrings[dir][e.Sel.Name]
		if !ok {
			continue
		}
		values = append(values, lit)
	}
	if len(values) == 0 {
		return nil, false
	}
	for _, v := range values[1:] {
		if v != values[0] {
			return nil, false
		}
	}
	return []string{values[0]}, true
}

// TestCapabilitySweepResolvesPackageQualifiedConst proves the sweep follows
// a package-qualified constant (leaf.LeafCapability) back to the declaring
// leaf's own directory. #6060 lane A moves the advisory capability consts
// out of root, so staying root handlers pass advisory.AdvisoryEvidenceCapability
// through queryselector.ResolveForRequestWithAccess; without this case the
// sweep reports the selector.go WriteGraphReadError call site unresolvable.
// It uses the real capabilitySweep machinery (not a re-implementation),
// like TestCapabilitySweepResolvesDeclarationsFromTheirOwnDirectory. The
// consumer imports the leaf package: without that import the selector must
// not resolve (see TestCapabilitySweepFailsClosedOnUnimportedSelector below).
func TestCapabilitySweepResolvesPackageQualifiedConst(t *testing.T) {
	root := t.TempDir()
	leafDir := filepath.Join(root, "leaf")
	consumerDir := filepath.Join(root, "consumer")
	if err := os.Mkdir(leafDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", leafDir, err)
	}
	if err := os.Mkdir(consumerDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", consumerDir, err)
	}

	const leafSrc = "package leaf\n\nconst LeafCapability = \"leaf.capability.list\"\n"
	const consumerSrc = "package consumer\n\nimport \"example.com/leaf\"\n\nfunc Use() string {\n\treturn leaf.LeafCapability\n}\n"

	leafPath := filepath.Join(leafDir, "leaf.go")
	consumerPath := filepath.Join(consumerDir, "consumer.go")
	if err := os.WriteFile(leafPath, []byte(leafSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", leafPath, err)
	}
	if err := os.WriteFile(consumerPath, []byte(consumerSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", consumerPath, err)
	}

	fset := token.NewFileSet()
	leafFile, err := parser.ParseFile(fset, leafPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", leafPath, err)
	}
	consumerFile, err := parser.ParseFile(fset, consumerPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", consumerPath, err)
	}

	sweep := newCapabilitySweep(fset)
	sweep.collectDecls(leafFile)
	sweep.collectDecls(consumerFile)

	fn := findFuncDeclByName(t, consumerFile, "Use")
	sel := findReturnedSelector(t, fn, "leaf", "LeafCapability")

	values, ok := sweep.resolveCapabilityArg(sel, fn, map[string]bool{})
	if !ok || len(values) != 1 || values[0] != "leaf.capability.list" {
		t.Fatalf("resolveCapabilityArg(leaf.LeafCapability) = %v, %v; want ([leaf.capability.list], true)", values, ok)
	}
}

// TestCapabilitySweepFailsClosedOnQualifiedConstDisagreement proves the
// fail-closed side of resolveQualifiedConst: two swept directories declaring
// the same package name with different values for the same constant do not
// resolve to whichever parsed last — they resolve to nothing, and the sweep
// reports the call site unresolvable for a human to disambiguate. Both
// directories match the consumer's import, so unanimity (not import binding)
// is what fails here.
func TestCapabilitySweepFailsClosedOnQualifiedConstDisagreement(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "x", "dup")
	dirB := filepath.Join(root, "y", "dup")
	consumerDir := filepath.Join(root, "consumer")
	for _, dir := range []string{dirA, dirB, consumerDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	const srcA = "package dup\n\nconst DupCapability = \"value-from-a\"\n"
	const srcB = "package dup\n\nconst DupCapability = \"value-from-b\"\n"
	const consumerSrc = "package consumer\n\nimport \"example.com/dup\"\n\nfunc Use() string {\n\treturn dup.DupCapability\n}\n"

	pathA := filepath.Join(dirA, "a.go")
	pathB := filepath.Join(dirB, "b.go")
	consumerPath := filepath.Join(consumerDir, "consumer.go")
	for path, src := range map[string]string{pathA: srcA, pathB: srcB, consumerPath: consumerSrc} {
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	fset := token.NewFileSet()
	sweep := newCapabilitySweep(fset)
	for _, path := range []string{pathA, pathB, consumerPath} {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		sweep.collectDecls(file)
		if path == consumerPath {
			fn := findFuncDeclByName(t, file, "Use")
			sel := findReturnedSelector(t, fn, "dup", "DupCapability")
			if _, ok := sweep.resolveCapabilityArg(sel, fn, map[string]bool{}); ok {
				t.Fatal("resolveCapabilityArg(dup.DupCapability) resolved across disagreeing packages; want unresolvable")
			}
		}
	}
}

// TestCapabilitySweepFailsClosedOnUnimportedSelector proves the import-binding
// side of resolveQualifiedConst: a pkg.Name selector in a file that imports
// nothing under that name does not resolve, even when a swept package of
// that exact name declares the constant. This is the shadowing-local shape:
// a local variable or struct value named like a leaf package must never
// resolve to the leaf's registered literal, or an unregistered capability
// value could ride through the matrix guard green.
func TestCapabilitySweepFailsClosedOnUnimportedSelector(t *testing.T) {
	root := t.TempDir()
	leafDir := filepath.Join(root, "leaf")
	consumerDir := filepath.Join(root, "consumer")
	if err := os.Mkdir(leafDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", leafDir, err)
	}
	if err := os.Mkdir(consumerDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", consumerDir, err)
	}

	const leafSrc = "package leaf\n\nconst LeafCapability = \"leaf.capability.list\"\n"
	const consumerSrc = "package consumer\n\ntype leafShadow struct {\n\tLeafCapability string\n}\n\nfunc Use(v leafShadow) string {\n\treturn v.LeafCapability\n}\n"

	leafPath := filepath.Join(leafDir, "leaf.go")
	consumerPath := filepath.Join(consumerDir, "consumer.go")
	if err := os.WriteFile(leafPath, []byte(leafSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", leafPath, err)
	}
	if err := os.WriteFile(consumerPath, []byte(consumerSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", consumerPath, err)
	}

	fset := token.NewFileSet()
	leafFile, err := parser.ParseFile(fset, leafPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", leafPath, err)
	}
	consumerFile, err := parser.ParseFile(fset, consumerPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", consumerPath, err)
	}

	sweep := newCapabilitySweep(fset)
	sweep.collectDecls(leafFile)
	sweep.collectDecls(consumerFile)

	fn := findFuncDeclByName(t, consumerFile, "Use")
	sel := findReturnedSelector(t, fn, "v", "LeafCapability")
	// The receiver here is the parameter v, not a package name: resolution
	// must fail before import binding is even consulted.
	if _, ok := sweep.resolveCapabilityArg(sel, fn, map[string]bool{}); ok {
		t.Fatal("resolveCapabilityArg(v.LeafCapability) resolved a struct-field selector; want unresolvable")
	}
}

// TestCapabilitySweepFailsClosedOnShadowedPackageName proves the exact hole
// the import-binding gate closes: a file that declares a LOCAL variable
// named exactly like an imported-sounding package (but imports nothing)
// must not resolve pkg.Name to the swept leaf's literal.
func TestCapabilitySweepFailsClosedOnShadowedPackageName(t *testing.T) {
	root := t.TempDir()
	leafDir := filepath.Join(root, "leaf")
	consumerDir := filepath.Join(root, "consumer")
	if err := os.Mkdir(leafDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", leafDir, err)
	}
	if err := os.Mkdir(consumerDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", consumerDir, err)
	}

	const leafSrc = "package leaf\n\nconst LeafCapability = \"leaf.capability.list\"\n"
	const consumerSrc = "package consumer\n\nfunc Use() string {\n\tleaf := map[string]string{\"LeafCapability\": \"unregistered\"}\n\treturn leafLeaf(leaf)\n}\n\nfunc leafLeaf(m map[string]string) string {\n\treturn m[\"LeafCapability\"]\n}\n"

	leafPath := filepath.Join(leafDir, "leaf.go")
	consumerPath := filepath.Join(consumerDir, "consumer.go")
	if err := os.WriteFile(leafPath, []byte(leafSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", leafPath, err)
	}
	if err := os.WriteFile(consumerPath, []byte(consumerSrc), 0o644); err != nil {
		t.Fatalf("write %s: %v", consumerPath, err)
	}

	fset := token.NewFileSet()
	leafFile, err := parser.ParseFile(fset, leafPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", leafPath, err)
	}
	consumerFile, err := parser.ParseFile(fset, consumerPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", consumerPath, err)
	}

	sweep := newCapabilitySweep(fset)
	sweep.collectDecls(leafFile)
	sweep.collectDecls(consumerFile)

	// No pkg.Name selector exists here at all (index expressions, not
	// selectors): assert the sweep finds no resolvable qualified const by
	// driving resolveQualifiedConst directly on a synthetic selector whose
	// receiver names the leaf package without an import behind it.
	sel := &ast.SelectorExpr{X: ast.NewIdent("leaf"), Sel: ast.NewIdent("LeafCapability")}
	if _, ok := sweep.resolveQualifiedConst(sel); ok {
		t.Fatal("resolveQualifiedConst(leaf.LeafCapability) resolved without a backing import; want unresolvable")
	}
}

// findReturnedSelector returns the first *ast.SelectorExpr of the shape
// pkg.Name appearing as a single-value return result inside fn, failing the
// test if none is found.
func findReturnedSelector(t *testing.T, fn *ast.FuncDecl, pkg string, name string) *ast.SelectorExpr {
	t.Helper()
	var found *ast.SelectorExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if sel, ok := ret.Results[0].(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkg && sel.Sel.Name == name {
				found = sel
				return false
			}
		}
		return true
	})
	if found == nil {
		t.Fatalf("no return of %s.%s in %s", pkg, name, fn.Name.Name)
	}
	return found
}
