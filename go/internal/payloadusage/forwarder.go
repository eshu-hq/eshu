// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RootForwarders maps a relocated decode seam's exported target identifier
// (e.g. "DecodeAWSResource", the name it is exported under in a subpackage
// such as schemadecode) back to the root-level compatibility name a package
// root still exposes it under (e.g. "decodeAWSResource"). Issue #6061 moved
// the reducer's per-fact-kind decoders into go/internal/reducer/schemadecode
// and left root-level forwarders — a var binding or a single-statement func
// wrapper — so the reducer's own handler call sites keep their original
// lowercase spelling. ParseDecodeSeams recognizes a seam under whatever name
// the file that declares it uses, which after the move is the subpackage's
// exported name; RootForwarders is what lets ResolveForwardedSeams translate
// that back to the identity the root package (and its handler call sites)
// actually use.
type RootForwarders map[string]string

// ParseRootForwarders scans every non-test *.go file directly inside dir —
// dir's own files only, NOT any subdirectory — for two compatibility-forwarder
// shapes a package root uses to keep a relocated function's original name
// callable:
//
//  1. A var binding: `var decodeX = <pkg>.DecodeX`.
//  2. A single-statement func wrapper:
//     `func decodeX(...) (...) { return <pkg>.DecodeX(...) }`.
//
// Both shapes are recognized by their RHS/return value being a
// package-qualified selector expression (`<pkg>.Name`); a var or func whose
// target is a bare local identifier is a root-local declaration, not a
// relocation forwarder, and is not recorded. The scan is intentionally
// non-recursive: the relocated seams themselves typically live one directory
// below dir (e.g. dir/schemadecode), and reading recursively would pull their
// declarations into the same map ParseRootForwarders is building, corrupting
// it. A dir with no forwarders at all (the pre-#6061 world, and every
// directory ParseRootForwarders is not pointed at) returns an empty map with
// no error, so callers can apply ResolveForwardedSeams unconditionally.
func ParseRootForwarders(dir string) (RootForwarders, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("payloadusage: read dir %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	forwarders := RootForwarders{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		// #nosec G304 -- path comes from os.ReadDir over a fixed reducer-root
		// dir, not from untrusted input.
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, fmt.Errorf("payloadusage: parse %s: %w", path, parseErr)
		}
		collectForwarders(file, forwarders)
	}
	return forwarders, nil
}

// collectForwarders walks file's top-level declarations and records every
// var-binding and func-wrapper forwarder it declares into forwarders (see
// ParseRootForwarders' doc comment for the two recognized shapes).
func collectForwarders(file *ast.File, forwarders RootForwarders) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			collectVarForwarders(d, forwarders)
		case *ast.FuncDecl:
			collectFuncForwarder(d, forwarders)
		}
	}
}

// collectVarForwarders records every `name = <pkg>.Target` value spec inside
// a `var` declaration (single or grouped in parens) as forwarders[Target] =
// name. A spec whose value is not a package-qualified selector (a bare local
// identifier, a literal, a call expression, ...) is skipped: it is an
// ordinary root-local var, not a relocation forwarder.
func collectVarForwarders(d *ast.GenDecl, forwarders RootForwarders) {
	if d.Tok != token.VAR {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) != len(vs.Values) {
			continue
		}
		for i, value := range vs.Values {
			target, ok := selectorTargetName(value)
			if !ok {
				continue
			}
			forwarders[target] = vs.Names[i].Name
		}
	}
}

// collectFuncForwarder records fn as a forwarder when its entire body is
// exactly one `return <pkg>.Target(...)` statement, the single-statement
// func-wrapper shape (e.g. factschemaEnvelope forwarding to
// schemadecode.FactschemaEnvelope). Any other body shape — more than one
// statement, a bare value return, a call to a local (unqualified) function —
// is not a relocation forwarder and is skipped.
func collectFuncForwarder(fn *ast.FuncDecl, forwarders RootForwarders) {
	if fn.Recv != nil || fn.Body == nil || len(fn.Body.List) != 1 {
		return
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return
	}
	target, ok := selectorTargetName(call.Fun)
	if !ok {
		return
	}
	forwarders[target] = fn.Name.Name
}

// selectorTargetName returns Name for a package-qualified selector
// expression (`<pkg>.Name`), or ok=false for any other expression shape (a
// bare identifier, a literal, an index or star expression, ...). This is
// what distinguishes a relocation forwarder — whose RHS/callee is always
// package-qualified, since the whole point is forwarding to another package
// — from an ordinary root-local declaration.
func selectorTargetName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, ok := sel.X.(*ast.Ident); !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// ResolveForwardedSeams returns seams with each seam's FuncName rewritten to
// forwarders[FuncName] when that target has a recorded root forwarder,
// leaving any seam with no matching forwarder unchanged. This is what lets a
// decode seam relocated into a subpackage (and so parsed under its exported
// name, e.g. "DecodeAWSResource") keep the identity its handler call sites
// actually use (e.g. "decodeAWSResource") — both for ScanDecodeUsage's
// call-site attribution and for mergeSeams' cross-stage FuncName join. An
// empty forwarders map (the pre-#6061 world, and every non-reducer decode
// surface) returns seams completely unchanged, still re-sorted for the same
// determinism ParseDecodeSeams itself guarantees.
func ResolveForwardedSeams(seams []DecodeSeam, forwarders RootForwarders) []DecodeSeam {
	resolved := make([]DecodeSeam, len(seams))
	for i, s := range seams {
		if rootName, ok := forwarders[s.FuncName]; ok {
			s.FuncName = rootName
		}
		resolved[i] = s
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].FuncName < resolved[j].FuncName })
	return resolved
}
