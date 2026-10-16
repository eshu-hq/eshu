// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
)

// queryVerdictStatusSources are the functions whose first result is a status
// that already applies the #7674 rules: 499 for a client cancel, 500 for a
// fault, or a graph-read or reader-fence verdict. They match under any
// qualifier.
var queryVerdictStatusSources = map[string]bool{
	"ServerFailureEnvelope":  true,
	"GraphReadErrorEnvelope": true,
}

// returnerIndex returns the result index at which call yields an allowed
// status: 0 for a queryVerdictStatusSources function, or the index derived
// for a tree function (deriveReturners). When the receiver type is not
// evident, every same-named method must be a returner with the same index.
func (tree *queryGuardTree) returnerIndex(f *queryGuardFile, call *ast.CallExpr) (int, bool) {
	target, ok := tree.target(f, call)
	if !ok {
		return 0, false
	}
	if queryVerdictStatusSources[target.base] {
		return 0, true
	}
	if len(target.keys) == 0 {
		return 0, false
	}
	index := -1
	for _, key := range target.keys {
		got, ok := tree.returners[key.dir][key.key]
		if !ok || (index >= 0 && got != index) {
			return 0, false
		}
		index = got
	}
	return index, true
}

// deriveReturners computes the fixed point of status returners: a function
// declaration with an int result at index k whose every return statement
// yields an allowed status at k. An allowed status is an allowed literal
// (queryAllowedDirectStatus), a local whose every assignment is allowed
// (localStatusAllowed), or, for a return of one multi-value call, a returner
// whose own index is k. A naked return or a forwarded parameter is not
// allowed. Returners only grow, so the loop ends.
func (tree *queryGuardTree) deriveReturners() {
	tree.returners = map[string]map[string]int{}
	for changed := true; changed; {
		changed = false
		for _, f := range tree.files {
			for _, decl := range f.file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Type.Results == nil {
					continue
				}
				if _, known := tree.returners[f.dir][queryFuncKey(fn)]; known {
					continue
				}
				index, ok := tree.statusResultIndex(f, fn)
				if !ok {
					continue
				}
				if tree.returners[f.dir] == nil {
					tree.returners[f.dir] = map[string]int{}
				}
				tree.returners[f.dir][queryFuncKey(fn)] = index
				changed = true
			}
		}
	}
}

// statusResultIndex returns the first int result index of fn at which every
// return statement yields an allowed status.
func (tree *queryGuardTree) statusResultIndex(f *queryGuardFile, fn *ast.FuncDecl) (int, bool) {
	var resultTypes []ast.Expr
	for _, field := range fn.Type.Results.List {
		for range max(1, len(field.Names)) {
			resultTypes = append(resultTypes, field.Type)
		}
	}
	var returns []*ast.ReturnStmt
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			returns = append(returns, n)
		}
		return true
	})
	if len(returns) == 0 {
		return 0, false
	}
	for k, typ := range resultTypes {
		if ident, ok := typ.(*ast.Ident); !ok || ident.Name != "int" {
			continue
		}
		allowed := true
		for _, ret := range returns {
			allowed = allowed && tree.returnYieldsAllowedStatus(f, fn.Body, ret, k, len(resultTypes))
		}
		if allowed {
			return k, true
		}
	}
	return 0, false
}

// returnYieldsAllowedStatus reports whether ret yields an allowed status at
// result index k of n results.
func (tree *queryGuardTree) returnYieldsAllowedStatus(f *queryGuardFile, body *ast.BlockStmt, ret *ast.ReturnStmt, k, n int) bool {
	switch {
	case len(ret.Results) == n:
		return tree.statusExprAllowed(f, body, ret.Results[k])
	case len(ret.Results) == 1 && n > 1:
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			return false
		}
		index, ok := tree.returnerIndex(f, call)
		return ok && index == k
	default:
		return false
	}
}

// statusExprAllowed reports whether expr, in body, is an allowed literal or a
// local whose every assignment is allowed.
func (tree *queryGuardTree) statusExprAllowed(f *queryGuardFile, body *ast.BlockStmt, expr ast.Expr) bool {
	if value, ok := queryStatusLiteral(expr); ok {
		return queryAllowedDirectStatus(value)
	}
	ident, ok := expr.(*ast.Ident)
	return ok && tree.localStatusAllowed(f, body, ident.Name)
}

// localStatusAllowed reports whether name is assigned in body and every
// assignment to it is allowed: an allowed literal (`status := http.StatusOK`,
// `status = http.StatusCreated`), or position i of a multi-value call whose
// returner index is i (`status, env := tracing.ServerFailureEnvelope(...)`).
// Anything else, an increment, or no assignment at all is not allowed.
func (tree *queryGuardTree) localStatusAllowed(f *queryGuardFile, body *ast.BlockStmt, name string) bool {
	if body == nil {
		return false
	}
	assigned, allowed := false, true
	check := func(i int, lhsCount int, rhs []ast.Expr) {
		assigned = true
		switch {
		case len(rhs) == lhsCount:
			value, ok := queryStatusLiteral(rhs[i])
			allowed = allowed && ok && queryAllowedDirectStatus(value)
		case len(rhs) == 1:
			call, ok := rhs[0].(*ast.CallExpr)
			index, isReturner := 0, false
			if ok {
				index, isReturner = tree.returnerIndex(f, call)
			}
			allowed = allowed && isReturner && index == i
		default:
			allowed = false
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == name {
					check(i, len(n.Lhs), n.Rhs)
				}
			}
		case *ast.IncDecStmt:
			if ident, ok := n.X.(*ast.Ident); ok && ident.Name == name {
				allowed = false
			}
		case *ast.ValueSpec:
			for i, ident := range n.Names {
				if ident.Name == name {
					check(i, len(n.Names), n.Values)
				}
			}
		}
		return true
	})
	return assigned && allowed
}
