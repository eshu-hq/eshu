// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// DecodeSeam describes one reducer-side decode<Kind> function discovered in
// go/internal/reducer/factschema_decode.go: which factschema.FactKind*
// constant it decodes and which typed struct (package-qualified, e.g.
// "awsv1.Resource") it returns. This is the derivation root the payload-usage
// manifest is built from — Contract System v1 §6 gate 2 requires the manifest
// come from the typed decode calls themselves, not from string literals
// scattered across handler files.
type DecodeSeam struct {
	// FuncName is the decode function's identifier, e.g. "decodeAWSResource".
	FuncName string
	// FactKindConst is the factschema.FactKind* identifier the function
	// decodes against, e.g. "FactKindAWSResource".
	FactKindConst string
	// StructPackage is the import alias of the returned struct's package,
	// e.g. "awsv1" or "iamv1".
	StructPackage string
	// StructName is the returned struct's bare type name, e.g. "Resource".
	StructName string
}

// QualifiedStruct returns the package-qualified struct type name, e.g.
// "awsv1.Resource", used as the join key between a DecodeSeam and its
// StructShape.
func (d DecodeSeam) QualifiedStruct() string {
	return d.StructPackage + "." + d.StructName
}

// ParseDecodeSeams parses path (expected to be
// go/internal/reducer/factschema_decode.go) and returns every decode<Kind>
// function it declares, sorted by FuncName for deterministic output.
//
// A decode function is recognized by this exact shape: a package-level func
// taking one facts.Envelope-typed parameter and returning
// (<pkg>.<Struct>, error). This mirrors the seam's own documented contract
// ("It is the single decode site for the <kind> kind on the reducer side")
// rather than matching on the "decode" name prefix alone, so a helper
// function that happens to start with "decode" but does not return a typed
// factschema struct is not misidentified as a seam.
func ParseDecodeSeams(path string) ([]DecodeSeam, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("payload-usage-manifest: parse %s: %w", path, err)
	}

	var seams []DecodeSeam
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		structPkg, structName, ok := decodeFuncReturnType(fn)
		if !ok {
			continue
		}
		factKindConst, ok := decodeFuncFactKindConst(fn)
		if !ok {
			continue
		}
		seams = append(seams, DecodeSeam{
			FuncName:      fn.Name.Name,
			FactKindConst: factKindConst,
			StructPackage: structPkg,
			StructName:    structName,
		})
	}

	sort.Slice(seams, func(i, j int) bool { return seams[i].FuncName < seams[j].FuncName })
	return seams, nil
}

// decodeFuncReturnType reports whether fn's signature is
// func(facts.Envelope) (<pkg>.<Struct>, error) and returns the qualified
// struct's package alias and bare name. Any other shape (wrong param count,
// wrong result count, non-error second result, unqualified first result)
// returns ok=false so non-seam helpers are skipped.
func decodeFuncReturnType(fn *ast.FuncDecl) (pkg, name string, ok bool) {
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return "", "", false
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
		return "", "", false
	}
	errIdent, isIdent := fn.Type.Results.List[1].Type.(*ast.Ident)
	if !isIdent || errIdent.Name != "error" {
		return "", "", false
	}
	sel, isSelector := fn.Type.Results.List[0].Type.(*ast.SelectorExpr)
	if !isSelector {
		return "", "", false
	}
	pkgIdent, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return pkgIdent.Name, sel.Sel.Name, true
}

// decodeFuncFactKindConst finds the factschema.FactKind* selector expression
// passed as the fact kind identifier to newFactDecodeError or
// factschema.Decode<Kind> inside fn's body, returning its bare constant name
// (e.g. "FactKindAWSResource"). This ties the decode function to the wire
// fact kind it decodes without hard-coding a name-based mapping between the
// Go func name and the fact kind string.
func decodeFuncFactKindConst(fn *ast.FuncDecl) (string, bool) {
	var found string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkgIdent.Name != "factschema" {
			return true
		}
		if !strings.HasPrefix(sel.Sel.Name, "FactKind") {
			return true
		}
		found = sel.Sel.Name
		return false
	})
	return found, found != ""
}
