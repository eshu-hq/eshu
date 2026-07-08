// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"go/ast"
	"go/token"
)

// wrapperSeamFields finds, across every parsed reducer-package file, the local
// (same-package) struct types that carry at least one field whose type is a
// decode seam's package-qualified struct — the "wrapper" structs a handler
// uses to pair a decoded typed payload with bookkeeping (iamPermissionStatement
// wraps iamv1.Permission alongside its source factID; secretsIAMPrincipal wraps
// iamv1.Principal alongside its envelope). It returns wrapper type name ->
// (wrapper field Go name -> the decode func that produces that field's type),
// so a later read of the form `wrapper.<seamField>.<StructField>` can be
// attributed to the right seam.
//
// Only bare value fields typed as a qualified seam struct match (via
// qualifiedTypeName): a pointer, slice, or map field type is not the
// single-value wrapper shape #4668 targets and is left to the documented
// attribution boundary. A wrapper type with no seam-typed field is omitted so
// the returned map contains only structs that can actually mediate a seam read.
func wrapperSeamFields(parsedFiles []parsedGoFile, structToFunc map[string]string) map[string]map[string]string {
	wrappers := map[string]map[string]string{}
	for _, pf := range parsedFiles {
		for _, decl := range pf.file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok || structType.Fields == nil {
					continue
				}
				fields := seamTypedFields(structType, structToFunc)
				if len(fields) > 0 {
					wrappers[typeSpec.Name.Name] = fields
				}
			}
		}
	}
	return wrappers
}

// seamTypedFields returns the subset of structType's named fields whose declared
// type is a package-qualified seam struct present in structToFunc, mapped to the
// decode func that struct came from.
func seamTypedFields(structType *ast.StructType, structToFunc map[string]string) map[string]string {
	fields := map[string]string{}
	for _, field := range structType.Fields.List {
		qualified, ok := qualifiedTypeName(field.Type)
		if !ok {
			continue
		}
		decodeFunc, ok := structToFunc[qualified]
		if !ok {
			continue
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			fields[name.Name] = decodeFunc
		}
	}
	return fields
}

// wrapperBoundIdentifiers finds every local identifier inside fn that is bound
// to a wrapper struct value, returning identifier name -> wrapper type name. It
// covers the shapes the migrated IAM/secrets_iam handlers actually use:
//
//   - a parameter typed as the wrapper struct `W` (the by-value helper shape,
//     `func inspect(statement iamPermissionStatement)`);
//   - a range value over a parameter or local typed `[]W` (the dominant shape,
//     `func build(statements []iamPermissionStatement) { for _, statement :=
//     range statements { ... } }`);
//   - a local declared `var x W` / `var x []W` or assigned a composite literal
//     `x := W{...}` / `x := []W{...}`.
//
// It deliberately does NOT resolve arbitrary aliasing, a range over a
// map-indexed expression, or a value returned from a call — those need full
// type information and are the documented attribution boundary, out of scope
// per #4668. Missing one of those only leaves a field unattributed (a lower
// bound), never misattributes.
func wrapperBoundIdentifiers(fn *ast.FuncDecl, wrappers map[string]map[string]string) map[string]string {
	bound := map[string]string{}     // ident -> wrapper type name
	sliceVars := map[string]string{} // ident -> wrapper element type name

	bindType := func(names []*ast.Ident, typ ast.Expr) {
		switch t := typ.(type) {
		case *ast.Ident:
			if _, ok := wrappers[t.Name]; ok {
				addNames(bound, names, t.Name)
			}
		case *ast.ArrayType:
			elt, ok := t.Elt.(*ast.Ident)
			if !ok {
				return
			}
			if _, ok := wrappers[elt.Name]; ok {
				addNames(sliceVars, names, elt.Name)
			}
		}
	}

	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			bindType(field.Names, field.Type)
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.DeclStmt:
			genDecl, ok := node.Decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				return true
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if valueSpec.Type != nil {
					bindType(valueSpec.Names, valueSpec.Type)
					continue
				}
				// Inferred declaration (`var x = W{...}`, `var xs = []W{...}`):
				// ValueSpec.Type is nil and the type lives on the composite
				// literal in Values. Pair each name with its value by index,
				// same as the `:=` composite-literal path.
				if len(valueSpec.Names) != len(valueSpec.Values) {
					continue
				}
				for i, name := range valueSpec.Names {
					if lit, ok := valueSpec.Values[i].(*ast.CompositeLit); ok && lit.Type != nil {
						bindType([]*ast.Ident{name}, lit.Type)
					}
				}
			}
		case *ast.AssignStmt:
			lit, lhs, ok := singleCompositeAssign(node)
			if ok {
				bindType([]*ast.Ident{lhs}, lit.Type)
			}
		case *ast.RangeStmt:
			x, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			elt, ok := sliceVars[x.Name]
			if !ok {
				return true
			}
			if value, ok := node.Value.(*ast.Ident); ok && value.Name != "_" {
				bound[value.Name] = elt
			}
		}
		return true
	})
	return bound
}

// singleCompositeAssign returns the composite literal and single LHS identifier
// of an assignment of the form `x := T{...}` (or `x = T{...}`), and ok=false
// for any other assignment shape.
func singleCompositeAssign(assign *ast.AssignStmt) (*ast.CompositeLit, *ast.Ident, bool) {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, nil, false
	}
	lhs, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || lhs.Name == "_" {
		return nil, nil, false
	}
	lit, ok := assign.Rhs[0].(*ast.CompositeLit)
	if !ok || lit.Type == nil {
		return nil, nil, false
	}
	return lit, lhs, true
}

// addNames records every non-blank identifier in names as mapping to value in
// dst.
func addNames(dst map[string]string, names []*ast.Ident, value string) {
	for _, name := range names {
		if name.Name == "_" {
			continue
		}
		dst[name.Name] = value
	}
}
