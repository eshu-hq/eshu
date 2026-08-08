// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// argocd_document_parse.go holds document parsing only: an ArgoCD
// Application/ApplicationSet map in, typed values out. Evidence emission stays
// in yaml_iac_evidence.go.
//
// That seam is the whole reason the split works, and it is the thing that
// stopped holding last time. yaml_iac_evidence.go reached 518 lines against a
// 500-line cap not in one change but by ratcheting — 502 at #5441's merge-base,
// 518 after — because each addition was individually small and nothing said
// where a new function belonged (#5573). A `//nolint:filelength` kept the gate
// quiet, and its stated reason cited an audit section tracking three other
// files (#5539).
//
// Splitting without naming the seam would set up the same ratchet with two
// files instead of one. So this asserts the rule rather than trusting it: no
// function in the parse file returns evidence. A function that needs to build
// an EvidenceFact belongs on the other side of the split.
func TestArgoCDDocumentParseFileEmitsNoEvidence(t *testing.T) {
	t.Parallel()

	// Resolve against this test file's own location rather than the working
	// directory: `go test` runs in the package dir, but a binary built with
	// `go test -c` and run elsewhere would silently find no file and pass.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate the package directory")
	}
	const name = "argocd_document_parse.go"
	path := filepath.Join(filepath.Dir(thisFile), name)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// Local names that ARE evidence: `type parsedEvidence = []EvidenceFact` and
	// `type parsedEvidence []EvidenceFact` both put a bare identifier in the
	// result AST, so matching only on "EvidenceFact" would let either through —
	// silently, while this test's comment promises the opposite. Collect them
	// first and treat them as evidence too (#5969 review, codex).
	evidenceAliases := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && returnsEvidenceFact(ts.Type, nil) {
				evidenceAliases[ts.Name.Name] = true
			}
		}
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Results == nil {
			continue
		}
		for _, result := range fn.Type.Results.List {
			if !returnsEvidenceFact(result.Type, evidenceAliases) {
				continue
			}
			t.Errorf("%s: %s returns evidence; this file parses documents and %s owns emission. "+
				"Putting emission here is how the original file ratcheted past the 500-line cap.",
				path, fn.Name.Name, "yaml_iac_evidence.go")
		}
	}
}

// returnsEvidenceFact reports whether a type is EvidenceFact, a slice or
// pointer to it, or a local name declared in this file as either — those are
// passed in via aliases, because a `type X = []EvidenceFact` puts only the bare
// identifier "X" in a function's result AST.
//
// It still does not chase names declared in OTHER files of the package. That is
// the residual: a helper returning a type aliased elsewhere would pass. Naming
// it here rather than claiming the check is total, because an earlier version
// of this comment promised that a new spelling "should fail this test loudly"
// while the code silently let an alias through — a promise the code did not
// keep is worse than a stated limit.
func returnsEvidenceFact(expr ast.Expr, aliases map[string]bool) bool {
	switch typed := expr.(type) {
	case *ast.ArrayType:
		return returnsEvidenceFact(typed.Elt, aliases)
	case *ast.StarExpr:
		return returnsEvidenceFact(typed.X, aliases)
	case *ast.Ident:
		return typed.Name == "EvidenceFact" || aliases[typed.Name]
	default:
		return false
	}
}
