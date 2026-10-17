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

// queryWriterKey names a function declaration in the walk: its package
// directory and its writer key (queryFuncKey).
type queryWriterKey struct {
	dir, key string
}

// String renders the key for a finding: "Recv.name" in the walk root,
// "dir/Recv.name" elsewhere.
func (k queryWriterKey) String() string {
	if k.dir == "." {
		return k.key
	}
	return k.dir + "/" + k.key
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

// indexTypes records, before any derivation, every method of the tree by
// name across all packages (methodsByName) and the named field types of every
// struct type (fields), so a method call can be resolved to its receiver type.
func (tree *queryGuardTree) indexTypes() {
	tree.methodsByName = map[string][]queryWriterKey{}
	tree.fields = map[queryTypeRef]map[string]queryTypeRef{}
	for _, f := range tree.files {
		for _, decl := range f.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					tree.methodsByName[d.Name.Name] = append(tree.methodsByName[d.Name.Name], queryWriterKey{dir: f.dir, key: queryFuncKey(d)})
				}
			case *ast.GenDecl:
				tree.indexStructFields(f, d)
			}
		}
	}
	for name, keys := range tree.methodsByName {
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		tree.methodsByName[name] = keys
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
// (pkg.Name for a qualified call), the bare name, http for http.Error, and
// the candidate declarations. unresolved is set when a method call's
// receiver type is not evident and keys is every method of that name in the
// tree, in any package.
type queryCallTarget struct {
	name, base string
	http       bool
	keys       []queryWriterKey
	unresolved bool
}

// target resolves call. A bare name is a plain function of f's package;
// pkg.Name a plain function of the imported package (no key for an import
// outside the walk root); x.name a method, keyed by x's type when exprType
// finds it, else by every method named name in every package of the tree
// (the conservative union).
func (tree *queryGuardTree) target(f *queryGuardFile, call *ast.CallExpr) (queryCallTarget, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return queryCallTarget{name: fun.Name, base: fun.Name, keys: []queryWriterKey{{dir: f.dir, key: fun.Name}}}, true
	case *ast.SelectorExpr:
		name := fun.Sel.Name
		if x, isIdent := fun.X.(*ast.Ident); isIdent {
			if x.Name == "http" && name == "Error" {
				return queryCallTarget{name: "http.Error", base: "Error", http: true}, true
			}
			if importDir, imported := f.imports[x.Name]; imported {
				target := queryCallTarget{name: x.Name + "." + name, base: name}
				if importDir != "" {
					target.keys = []queryWriterKey{{dir: importDir, key: name}}
				}
				return target, true
			}
		}
		if ref, ok := tree.exprType(f, queryEnclosingFunc(f, call), fun.X); ok {
			key := queryWriterKey{dir: ref.dir, key: ref.name + "." + name}
			return queryCallTarget{name: ref.name + "." + name, base: name, keys: []queryWriterKey{key}}, true
		}
		keys := tree.methodsByName[name]
		return queryCallTarget{name: name, base: name, keys: keys, unresolved: len(keys) > 0}, true
	default:
		return queryCallTarget{}, false
	}
}

// ambiguityNote is the text a finding carries when its writer was matched by
// the conservative union of same-named methods.
func (target queryCallTarget) ambiguityNote() string {
	if !target.unresolved {
		return ""
	}
	names := make([]string, 0, len(target.keys))
	for _, key := range target.keys {
		names = append(names, key.String())
	}
	return " [receiver type unresolved; matched " + strings.Join(names, ", ") + "]"
}
