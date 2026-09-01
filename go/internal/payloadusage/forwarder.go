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

// DecodeQualifiers is the set of package-alias identifiers decodeCallName
// accepts as a legitimate qualifier for a package-qualified decode call
// (pkg.DecodeX(env)). decodeFuncs (the set a resolved call name is joined
// against) is a bare name-keyed set with no package information — see
// ScanDecodeUsage's PACKAGE ISOLATION doc — and effectiveDecodeFuncs only
// strips a same-named conflict declared in the SAME package group as the
// call site. Neither guards a qualified call whose selector's package is not
// a real decode source at all: without this set, an unrelated package that
// happens to declare a same-named DecodeX (a coincidence, not a relocation)
// would silently misattribute its field reads to the real seam. Membership
// is checked by the bare identifier as it appears at the call site (the
// import alias, not the resolved import path); see KnownDecodeQualifiers.
type DecodeQualifiers map[string]struct{}

// KnownDecodeQualifiers is the DecodeQualifiers every ScanDecodeUsage caller
// in this codebase passes: the two packages a real qualified decode call
// site is measured to use as of #6372, across every scanned surface
// (reducer, projector, query, loader, relationships, replay) —
// "schemadecode" (go/internal/reducer/schemadecode, a family package calling
// its relocated seam directly per that package's "Adding a decoder"
// guidance) and "factschema" (sdk/go/factschema, the SDK-level decode a
// non-reducer consumer such as go/internal/storage/postgres's secrets_iam
// trust-chain anchor decoder calls directly, bypassing schemadecode
// entirely). This is an invariant to KEEP true, not a claim that it always
// will be: a third package hoisting its own qualified decode call sites
// needs its alias added here, or decodeCallName silently stops recognizing
// it — the same silent-undercount failure mode this set exists to close on
// the OTHER side (an unknown qualifier), not to reopen on this one.
var KnownDecodeQualifiers = DecodeQualifiers{
	"schemadecode": {},
	"factschema":   {},
}

// decodeCallName returns the decode-function identity a call expression's Fun
// resolves to, recognizing both call shapes recordDecodeBindings must
// attribute: an unqualified root-forwarder call (`decodeX(env)`, an
// *ast.Ident) and a package-qualified call
// (`schemadecode.DecodeX(env)`, an *ast.SelectorExpr) — the shape
// schemadecode/AGENTS.md instructs a family package to write once its seam
// has moved into that subpackage with no root-level forwarder left behind
// (#6372).
//
// A qualified call is recognized ONLY when its selector's qualifier
// identifier is a member of qualifiers (see DecodeQualifiers and
// KnownDecodeQualifiers) — an *ast.SelectorExpr through any other package is
// rejected outright, before decodeFuncs is ever consulted, closing the
// specific hole review found in #6372 round 2: a same-named function
// declared in a package that is not a real decode source at all (an
// arbitrary coincidence, not a relocation) can no longer be mistaken for the
// real seam.
//
// For a qualifier that passes that check, the selector's Sel.Name is
// resolved two ways: first through forwarders (Target -> root name), so a
// seam that DOES still have a root forwarder is recognized under the same
// root identity ResolveForwardedSeams already rewrote its DecodeSeam.FuncName
// to, even when this particular call site uses the exported subpackage
// spelling instead of the root one; when forwarders has no entry (the common
// case per schemadecode's "a family package should import this package
// directly" guidance — a forwarder is added only when a root call site still
// needs one), Sel.Name itself is returned, matching a seam whose FuncName was
// never rewritten because it was never forwarded in the first place. Either
// way the caller still joins the returned name against decodeFuncs.
//
// What this does NOT guarantee: decodeFuncs (see ScanDecodeUsage's PACKAGE
// ISOLATION doc) is a bare name-keyed set with no package information, and
// qualifiers only screens which PACKAGES are accepted — it does not verify
// that the name a given qualified call resolves to was actually declared in
// THAT package rather than another accepted one. Two accepted-qualifier
// packages (schemadecode and factschema) COULD in principle both declare a
// same-named DecodeX seam for two different fact kinds, and a qualified call
// into either would then misattribute against whichever one decodeFuncs
// happens to map that name to. The actual invariant this code relies on is
// weaker than "never binds wrong": no seam name collides across the scanned
// tree today (measured across all 125 kinds, reducer + projector + query +
// loader + relationships + replay) — not a property this code enforces or
// can detect a violation of.
func decodeCallName(fun ast.Expr, forwarders RootForwarders, qualifiers DecodeQualifiers) (string, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, true
	case *ast.SelectorExpr:
		pkgIdent, ok := f.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if _, known := qualifiers[pkgIdent.Name]; !known {
			return "", false
		}
		if rootName, ok := forwarders[f.Sel.Name]; ok {
			return rootName, true
		}
		return f.Sel.Name, true
	default:
		return "", false
	}
}
