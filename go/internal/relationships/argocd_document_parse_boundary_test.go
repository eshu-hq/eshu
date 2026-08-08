// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"go/ast"
	"go/parser"
	"go/token"
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

	const path = "argocd_document_parse.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Results == nil {
			continue
		}
		for _, result := range fn.Type.Results.List {
			if !returnsEvidenceFact(result.Type) {
				continue
			}
			t.Errorf("%s: %s returns evidence; this file parses documents and %s owns emission. "+
				"Putting emission here is how the original file ratcheted past the 500-line cap.",
				path, fn.Name.Name, "yaml_iac_evidence.go")
		}
	}
}

// returnsEvidenceFact reports whether a result type is EvidenceFact or a
// slice of them. It deliberately does not chase aliases or named wrappers —
// a new spelling for the same thing should fail this test loudly and be
// added here on purpose, rather than slip through a clever match.
func returnsEvidenceFact(expr ast.Expr) bool {
	switch typed := expr.(type) {
	case *ast.ArrayType:
		return returnsEvidenceFact(typed.Elt)
	case *ast.StarExpr:
		return returnsEvidenceFact(typed.X)
	case *ast.Ident:
		return typed.Name == "EvidenceFact"
	default:
		return false
	}
}
