// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// queryplanProfileLiveSourceFile is the live query-plan PROFILE gate this file
// guards. It is behind the queryplan_profile_live build tag and needs a Neo4j
// container to run, so the guard below reads it as source rather than running
// it: whether the gate's phases share a deadline is a property of the code, and
// a property of the code can be checked on every PR, on any machine, in
// milliseconds.
const queryplanProfileLiveSourceFile = "queryplan_profile_live_test.go"

// queryplanProfileLiveTestFunc is the gate whose phases must not share a clock.
const queryplanProfileLiveTestFunc = "TestProductionQueryplanProfilesRejectWholeGraphScans"

// queryplanProfileSchemaFunc applies the gate's schema, including
// `CALL db.awaitIndexes(120)` -- the single statement that could spend the old
// shared 2-minute budget on its own.
const queryplanProfileSchemaFunc = "applyQueryplanProfileSchema"

// queryplanProfileCtxScope is what one lexical scope of the live gate does with
// contexts: which context variables it derives a deadline for, and which
// context variables it hands to the graph driver.
type queryplanProfileCtxScope struct {
	derived  map[string]bool
	passed   map[string]bool
	callSite int
}

// queryplanProfileScopeOf reads one function body's context use, without
// descending into nested closures: a subtest closure's deadlines are that
// closure's business, and the enclosing body must not be credited with them.
func queryplanProfileScopeOf(body *ast.BlockStmt) queryplanProfileCtxScope {
	scope := queryplanProfileCtxScope{derived: map[string]bool{}, passed: map[string]bool{}}
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			name, ok := queryplanProfileDerivedCtxName(typed)
			if ok {
				scope.derived[name] = true
			}
		case *ast.CallExpr:
			name, ok := queryplanProfileGraphCallCtxName(typed)
			if ok {
				scope.passed[name] = true
				scope.callSite++
			}
		}
		return true
	})
	return scope
}

// queryplanProfileDerivedCtxName reports the variable an assignment gives its
// own deadline to, as in `ctx, cancel := context.WithTimeout(parent, budget)`.
func queryplanProfileDerivedCtxName(assign *ast.AssignStmt) (string, bool) {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return "", false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "context" {
		return "", false
	}
	if selector.Sel.Name != "WithTimeout" && selector.Sel.Name != "WithDeadline" {
		return "", false
	}
	target, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return target.Name, true
}

// queryplanProfileGraphCallCtxName reports the context variable handed to a
// graph driver call. Both `session.Run(ctx, ...)` and `result.Consume(ctx)`
// carry a deadline, so both count. `t.Run(name, func(...))` does not: it is
// matched by the closure argument and skipped.
func queryplanProfileGraphCallCtxName(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if selector.Sel.Name != "Run" && selector.Sel.Name != "Consume" {
		return "", false
	}
	if len(call.Args) == 2 {
		if _, isSubtest := call.Args[1].(*ast.FuncLit); isSubtest {
			return "", false
		}
	}
	if len(call.Args) == 0 {
		return "", false
	}
	ctx, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return ctx.Name, true
}

// TestQueryplanProfileLiveGatePhasesOwnTheirDeadlines is the guard for the
// shared-deadline defect: the live gate used to build one 2-minute context and
// hand it to schema setup AND to all ~516 PROFILE subtests, so index population
// could spend the budget and hundreds of subtests then failed with the driver's
// "Timeout while waiting for connection" wording -- a message that blames the
// container for an exhausted clock.
//
// The property is structural, so this asserts it structurally, hermetically,
// and on every PR: every phase of the gate that talks to the graph must derive
// its own deadline in its own scope.
func TestQueryplanProfileLiveGatePhasesOwnTheirDeadlines(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, queryplanProfileLiveSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", queryplanProfileLiveSourceFile, err)
	}

	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			funcs[fn.Name.Name] = fn
		}
	}
	// A rename or a file move must fail this guard loudly rather than let it
	// pass having checked nothing.
	for _, name := range []string{queryplanProfileLiveTestFunc, queryplanProfileSchemaFunc} {
		if funcs[name] == nil {
			t.Fatalf("%s no longer declares %s; this guard would pass having checked nothing -- point it at the new name",
				queryplanProfileLiveSourceFile, name)
		}
	}

	assertQueryplanProfileScopeOwnsDeadlines(t, queryplanProfileSchemaFunc, queryplanProfileScopeOf(funcs[queryplanProfileSchemaFunc].Body))

	liveScope := queryplanProfileScopeOf(funcs[queryplanProfileLiveTestFunc].Body)
	if liveScope.callSite != 0 {
		t.Errorf("%s issues %d graph call(s) in its own body; every graph call belongs in a phase that owns a deadline",
			queryplanProfileLiveTestFunc, liveScope.callSite)
	}

	subtests := queryplanProfileSubtestScopes(file)
	// The gate profiles manifest entries in one loop and production variants in
	// another, so fewer than two deadline-owning subtest closures means the
	// guard lost its subject.
	if len(subtests) < 2 {
		t.Fatalf("found %d subtest closure(s) issuing graph calls in %s, want at least 2 -- the guard has lost its subject",
			len(subtests), queryplanProfileLiveSourceFile)
	}
	for _, scope := range subtests {
		assertQueryplanProfileScopeOwnsDeadlines(t, "subtest closure", scope)
	}
}

// queryplanProfileSubtestScopes returns the context use of every subtest
// closure in the file that talks to the graph.
func queryplanProfileSubtestScopes(file *ast.File) []queryplanProfileCtxScope {
	var scopes []queryplanProfileCtxScope
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Run" {
			return true
		}
		closure, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}
		scope := queryplanProfileScopeOf(closure.Body)
		if scope.callSite > 0 {
			scopes = append(scopes, scope)
		}
		return true
	})
	return scopes
}

// assertQueryplanProfileScopeOwnsDeadlines fails when a scope hands the graph
// driver a context it did not give a deadline of its own -- the exact shape of
// the shared-clock defect.
func assertQueryplanProfileScopeOwnsDeadlines(t *testing.T, label string, scope queryplanProfileCtxScope) {
	t.Helper()
	if scope.callSite == 0 {
		t.Errorf("%s issues no graph call; this assertion would prove nothing", label)
		return
	}
	borrowed := make([]string, 0, len(scope.passed))
	for name := range scope.passed {
		if !scope.derived[name] {
			borrowed = append(borrowed, name)
		}
	}
	sort.Strings(borrowed)
	if len(borrowed) > 0 {
		t.Errorf("%s hands the graph driver context(s) %v that it did not give a deadline of its own. "+
			"A context borrowed from an enclosing scope shares that scope's clock: when it expires, every "+
			"remaining call fails at once with the driver's connectivity wording instead of one honest "+
			"deadline message. Derive the deadline here with context.WithTimeout.", label, borrowed)
	}
}
