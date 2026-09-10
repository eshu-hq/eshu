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
	"testing"
)

// resolveCallResult collects every literal value a call expression can
// return. A bare-identifier call reaches only the same package, so the
// callee is looked up in the call site's own directory. A
// package-qualified call (leaf.Func from a #6060 root forwarder) is bound
// through the consuming file's imports to the declaring leaf's directory
// by resolveQualifiedDir. Anything else (a method value, an indirect call)
// fails closed.
func (s *capabilitySweep) resolveCallResult(e *ast.CallExpr, visitedFuncs map[string]bool) ([]string, bool) {
	switch callee := e.Fun.(type) {
	case *ast.Ident:
		return s.resolveFuncReturns(callee.Name, s.dirOf(e.Pos()), visitedFuncs)
	case *ast.SelectorExpr:
		if callee.Sel == nil {
			return nil, false
		}
		dir, ok := s.resolveQualifiedDir(callee)
		if !ok {
			return nil, false
		}
		return s.resolveFuncReturns(callee.Sel.Name, dir, visitedFuncs)
	default:
		return nil, false
	}
}

// resolveQualifiedDir binds a pkg.Func call's package qualifier to the swept
// directory that declares it, through the same two gates as
// resolveQualifiedConst: the consuming file must import a package under the
// qualifier's spelling (import binding), and among swept directories
// declaring that package name only ones whose final path element matches the
// imported path's base qualify (directory binding). It fails closed unless
// exactly one directory qualifies -- a qualified call into an ambiguous
// package set resolves to nothing rather than to whichever parsed last, the
// same unanimity discipline resolveQualifiedConst enforces on values.
func (s *capabilitySweep) resolveQualifiedDir(e *ast.SelectorExpr) (string, bool) {
	pkgIdent, ok := e.X.(*ast.Ident)
	if !ok || e.Sel == nil {
		return "", false
	}
	consumer := s.fset.Position(e.Pos()).Filename
	importPath, ok := s.fileImports[consumer][pkgIdent.Name]
	if !ok {
		return "", false
	}
	wantBase := path.Base(importPath)
	dir := ""
	matches := 0
	for d, pkgName := range s.packageNames {
		if pkgName != pkgIdent.Name {
			continue
		}
		if filepath.Base(d) != wantBase {
			continue
		}
		dir = d
		matches++
	}
	if matches != 1 {
		return "", false
	}
	return dir, true
}

// Package-qualified CALL resolution lives here, split out of
// graph_read_error_capability_sweep_resolve_test.go once that file reached
// the repo's 500-line cap: resolveCallResult (the *ast.CallExpr branch of
// resolveCapabilityArg) plus resolveQualifiedDir, the import-and-directory
// binding that lets the sweep follow a leaf.Func(...) call from a root
// forwarder back into the declaring leaf's own directory.
//
// The #6060 leaf shape this guards: root keeps a thin forwarder such as
// relationshipCapability that delegates to relationships.Capability, so the
// WriteGraphReadError call sites pass relationshipCapability(...)'s return
// through. Resolving only bare-identifier callees reports every one of
// those sites unresolvable and fails the sweep on a tree that is actually
// clean.

// TestCapabilitySweepResolvesPackageQualifiedCall proves the sweep follows a
// package-qualified call (leaf.Capability(kind)) back to the declaring
// leaf's own directory and collects the returned literal. Without the
// SelectorExpr branch in resolveCallResult the call falls through to
// unresolvable, which is exactly how the relationships/ leaf8 move failed
// the sweep: relationshipCapability forwards to
// relationships.Capability(direction, relationshipType).
func TestCapabilitySweepResolvesPackageQualifiedCall(t *testing.T) {
	root := t.TempDir()
	leafDir := filepath.Join(root, "leaf")
	consumerDir := filepath.Join(root, "consumer")
	if err := os.Mkdir(leafDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", leafDir, err)
	}
	if err := os.Mkdir(consumerDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", consumerDir, err)
	}

	const leafSrc = "package leaf\n\nconst LeafCapability = \"leaf.capability.list\"\n\nfunc Capability(kind string) string {\n\treturn LeafCapability\n}\n"
	const consumerSrc = "package consumer\n\nimport \"example.com/leaf\"\n\nfunc Forward(kind string) string {\n\treturn leaf.Capability(kind)\n}\n\nfunc Use() string {\n\treturn Forward(\"leaf.capability.list\")\n}\n"

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

	fn := findFuncDeclByName(t, consumerFile, "Forward")
	call := findReturnedCall(t, fn)

	values, ok := sweep.resolveCapabilityArg(call, fn, map[string]bool{})
	if !ok || len(values) != 1 || values[0] != "leaf.capability.list" {
		t.Fatalf("resolveCapabilityArg(leaf.Capability(kind)) = %v, %v; want ([leaf.capability.list], true)", values, ok)
	}
}

// findReturnedCall returns the first *ast.CallExpr appearing as a
// single-value return result inside fn, failing the test if none is found.
func findReturnedCall(t *testing.T, fn *ast.FuncDecl) *ast.CallExpr {
	t.Helper()
	var found *ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if call, ok := ret.Results[0].(*ast.CallExpr); ok {
			found = call
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no returned call in %s", fn.Name.Name)
	}
	return found
}
