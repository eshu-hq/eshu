// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"sort"
	"strings"
)

// queryTypeRef names a type declared in the walk: its package directory and
// its name.
type queryTypeRef struct {
	dir, name string
}

// queryFuncKey is the writer key of a function declaration: "Recv.name" for a
// method on the named type Recv, "name" for a plain function.
func queryFuncKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return queryTypeExprName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

// queryTypeExprName returns the bare type name of T, *T, T[P], or pkg.T.
func queryTypeExprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return queryTypeExprName(e.X)
	case *ast.IndexExpr:
		return queryTypeExprName(e.X)
	case *ast.IndexListExpr:
		return queryTypeExprName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

// typeRef resolves a type expression written in f to the type it names: T in
// f's package, or pkg.T in an imported package under the walk root.
func queryTypeRefOf(f *queryGuardFile, expr ast.Expr) (queryTypeRef, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return queryTypeRef{dir: f.dir, name: e.Name}, true
	case *ast.StarExpr:
		return queryTypeRefOf(f, e.X)
	case *ast.IndexExpr:
		return queryTypeRefOf(f, e.X)
	case *ast.IndexListExpr:
		return queryTypeRefOf(f, e.X)
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok {
			return queryTypeRef{}, false
		}
		if dir, imported := f.imports[pkg.Name]; imported && dir != "" {
			return queryTypeRef{dir: dir, name: e.Sel.Name}, true
		}
	}
	return queryTypeRef{}, false
}

// indexTypes records, before any derivation, every method name per package
// (methodsByName) and the named field types of every struct type (fields), so
// a method call can be resolved to its receiver type.
func (tree *queryGuardTree) indexTypes() {
	tree.methodsByName = map[string]map[string][]string{}
	tree.fields = map[queryTypeRef]map[string]queryTypeRef{}
	for _, f := range tree.files {
		for _, decl := range f.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					continue
				}
				if tree.methodsByName[f.dir] == nil {
					tree.methodsByName[f.dir] = map[string][]string{}
				}
				tree.methodsByName[f.dir][d.Name.Name] = append(tree.methodsByName[f.dir][d.Name.Name], queryFuncKey(d))
			case *ast.GenDecl:
				tree.indexStructFields(f, d)
			}
		}
	}
	for _, pkg := range tree.methodsByName {
		for name, keys := range pkg {
			sort.Strings(keys)
			pkg[name] = keys
		}
	}
}

// indexStructFields records the named, typed fields of each struct type
// declared in d.
func (tree *queryGuardTree) indexStructFields(f *queryGuardFile, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		owner := queryTypeRef{dir: f.dir, name: typeSpec.Name.Name}
		for _, field := range structType.Fields.List {
			ref, ok := queryTypeRefOf(f, field.Type)
			if !ok {
				continue
			}
			for _, name := range field.Names {
				if tree.fields[owner] == nil {
					tree.fields[owner] = map[string]queryTypeRef{}
				}
				tree.fields[owner][name.Name] = ref
			}
		}
	}
}

// queryEnclosingFunc returns the function declaration in f that contains
// node, or nil.
func queryEnclosingFunc(f *queryGuardFile, node ast.Node) *ast.FuncDecl {
	for _, decl := range f.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Pos() <= node.Pos() && node.End() <= fn.End() {
			return fn
		}
	}
	return nil
}

// exprType returns the type of a receiver expression when it is statically
// evident inside fn: the method's receiver, a parameter or a local declared
// with a type or a composite literal, or a field of a struct of known type.
func (tree *queryGuardTree) exprType(f *queryGuardFile, fn *ast.FuncDecl, expr ast.Expr) (queryTypeRef, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return tree.exprType(f, fn, e.X)
	case *ast.StarExpr:
		return tree.exprType(f, fn, e.X)
	case *ast.Ident:
		return queryIdentType(f, fn, e.Name)
	case *ast.SelectorExpr:
		owner, ok := tree.exprType(f, fn, e.X)
		if !ok {
			return queryTypeRef{}, false
		}
		field, ok := tree.fields[owner][e.Sel.Name]
		return field, ok
	default:
		return queryTypeRef{}, false
	}
}

// queryIdentType returns the declared type of name in fn: its receiver, a
// parameter, a `var name T`, or `name := T{...}` / `name := &T{...}`.
func queryIdentType(f *queryGuardFile, fn *ast.FuncDecl, name string) (queryTypeRef, bool) {
	if fn == nil {
		return queryTypeRef{}, false
	}
	var lists []*ast.FieldList
	if fn.Recv != nil {
		lists = append(lists, fn.Recv)
	}
	lists = append(lists, fn.Type.Params)
	for _, list := range lists {
		for _, field := range list.List {
			for _, ident := range field.Names {
				if ident.Name == name {
					return queryTypeRefOf(f, field.Type)
				}
			}
		}
	}
	var ref queryTypeRef
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ValueSpec:
			for _, ident := range n.Names {
				if ident.Name == name && n.Type != nil {
					ref, found = queryTypeRefOf(f, n.Type)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name != name || len(n.Rhs) != len(n.Lhs) {
					continue
				}
				rhs := n.Rhs[i]
				if unary, ok := rhs.(*ast.UnaryExpr); ok {
					rhs = unary.X
				}
				if lit, ok := rhs.(*ast.CompositeLit); ok && lit.Type != nil {
					ref, found = queryTypeRefOf(f, lit.Type)
				}
			}
		}
		return !found
	})
	return ref, found
}

// queryCallTarget is the function a call may invoke: the name as written
// (pkg.Name for a qualified call), the bare name, the package directory a
// tree function lives in ("" for an import outside the walk root, "http" for
// http.Error), and the candidate writer keys in that directory. ambiguous
// lists every candidate when a method call's receiver type is not evident and
// the package declares more than one method by that name.
type queryCallTarget struct {
	name, base, dir string
	keys            []string
	ambiguous       bool
}

// target resolves call. A bare name is a plain function of f's package;
// pkg.Name a plain function of the imported package; x.name a method, keyed
// by x's type when exprType finds it, else by every method named name in f's
// package (the conservative union).
func (tree *queryGuardTree) target(f *queryGuardFile, call *ast.CallExpr) (queryCallTarget, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return queryCallTarget{name: fun.Name, base: fun.Name, dir: f.dir, keys: []string{fun.Name}}, true
	case *ast.SelectorExpr:
		name := fun.Sel.Name
		if x, isIdent := fun.X.(*ast.Ident); isIdent {
			if x.Name == "http" && name == "Error" {
				return queryCallTarget{name: "http.Error", base: "Error", dir: "http"}, true
			}
			if importDir, imported := f.imports[x.Name]; imported {
				return queryCallTarget{name: x.Name + "." + name, base: name, dir: importDir, keys: []string{name}}, true
			}
		}
		if ref, ok := tree.exprType(f, queryEnclosingFunc(f, call), fun.X); ok {
			return queryCallTarget{name: ref.name + "." + name, base: name, dir: ref.dir, keys: []string{ref.name + "." + name}}, true
		}
		keys := tree.methodsByName[f.dir][name]
		return queryCallTarget{name: name, base: name, dir: f.dir, keys: keys, ambiguous: len(keys) > 1}, true
	default:
		return queryCallTarget{}, false
	}
}

// ambiguityNote is the text a finding carries when its writer was matched by
// the conservative union of same-named methods.
func (target queryCallTarget) ambiguityNote() string {
	if !target.ambiguous {
		return ""
	}
	return " [receiver type unresolved; matched " + strings.Join(target.keys, ", ") + "]"
}
