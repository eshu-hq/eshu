// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"time"
)

// This file is the source reader behind
// TestQueryplanProfileLiveGatePhasesOwnTheirDeadlines, which lives in
// queryplan_profile_deadline_guard_test.go and makes every assertion. Here we
// only turn one lexical scope of the live PROFILE gate into facts: which
// contexts were given a deadline there, which budget each deadline was built
// from, which parent it was derived from, and which contexts reached the graph
// driver. The two files are split because together they exceed the repo's
// 500-line cap.

// queryplanProfileCtxScope is what one lexical scope of the live gate does with
// contexts: which context variables it derives a deadline for from a clock-free
// parent, which it derives from a parent that may already be running down a
// clock, which budget each of those deadlines was built from, and which it hands
// to the graph driver.
type queryplanProfileCtxScope struct {
	derived map[string]bool
	// chained maps a context variable to the parent it took its deadline from,
	// when that parent is not a deadline-free root. context.WithTimeout keeps
	// whichever deadline is EARLIER, so such a context is still on the parent's
	// clock no matter what budget it was given.
	chained map[string]string
	// budget maps a context variable to the second argument its deadline was
	// built from, rendered as the author wrote it. A phase that passes a
	// hardcoded duration, or another phase's constant, is not on the budget its
	// documentation claims -- and neither the structural check nor the
	// constant-ordering test can see that, because both read past this argument.
	budget map[string]string
	passed map[string]bool
	// opaqueCtx lists driver calls whose context argument is not a plain
	// variable, rendered as written. Such a call used to be dropped on the floor
	// by this reader, which meant `session.Run(context.Background(), ...)` --
	// a profiled query with no deadline at all -- left no trace to assert on.
	opaqueCtx []string
	// profileSite counts calls that run a profiled query (Run, Consume). Those
	// belong in a subtest, never in the gate's own body.
	profileSite int
	// driverSite counts every call that hands the driver a context, profiled
	// query or not. VerifyConnectivity is one: the connect phase talks to the
	// graph on a deadline like any other phase, so its context is held to the
	// same rule.
	driverSite int
}

// Names of the budget constants the live gate's phases derive their deadlines
// from. They are strings because the guard compares them against identifiers
// read out of source, where a constant is only ever a name.
const (
	queryplanProfileConnectBudgetName = "queryplanProfileConnectBudget"
	queryplanProfileSchemaBudgetName  = "queryplanProfileSchemaBudget"
	queryplanProfileQueryBudgetName   = "queryplanProfileQueryBudget"
	queryplanProfileTotalBudgetName   = "queryplanProfileTotalBudget"
)

// queryplanProfileBudgetConsts is the closed set of budget constants a phase of
// the live gate may build a deadline from, mapped to the value each one holds.
//
// The values are read from the constants themselves rather than copied, so this
// map cannot go on naming a constant that no longer exists: delete or rename one
// and the guard stops compiling instead of quietly matching a dead string.
var queryplanProfileBudgetConsts = map[string]time.Duration{
	queryplanProfileConnectBudgetName: queryplanProfileConnectBudget,
	queryplanProfileSchemaBudgetName:  queryplanProfileSchemaBudget,
	queryplanProfileQueryBudgetName:   queryplanProfileQueryBudget,
	queryplanProfileTotalBudgetName:   queryplanProfileTotalBudget,
}

// queryplanProfileScopeOf reads one function body's context use, without
// descending into nested closures: a subtest closure's deadlines are that
// closure's business, and the enclosing body must not be credited with them.
func queryplanProfileScopeOf(body *ast.BlockStmt) queryplanProfileCtxScope {
	scope := queryplanProfileCtxScope{
		derived: map[string]bool{},
		chained: map[string]string{},
		budget:  map[string]string{},
		passed:  map[string]bool{},
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			name, parent, budget, ok := queryplanProfileDerivedCtx(typed)
			switch {
			case !ok:
			case queryplanProfileIsDeadlineFreeRoot(parent):
				scope.derived[name] = true
				scope.budget[name] = budget
			default:
				scope.chained[name] = queryplanProfileExprLabel(parent)
				scope.budget[name] = budget
			}
		case *ast.CallExpr:
			ctx, profiled, ok := queryplanProfileGraphCallCtx(typed)
			if !ok {
				return true
			}
			scope.driverSite++
			if profiled {
				scope.profileSite++
			}
			if name, isIdent := ctx.(*ast.Ident); isIdent {
				scope.passed[name.Name] = true
			} else {
				scope.opaqueCtx = append(scope.opaqueCtx, queryplanProfileExprLabel(ctx))
			}
		}
		return true
	})
	return scope
}

// queryplanProfileDerivedCtx reports the variable an assignment gives a deadline
// to, the parent that deadline was derived from, and the budget it was built
// with, as in `ctx, cancel := context.WithTimeout(parent, budget)`.
//
// Neither argument is discarded, because each decides a different thing.
//
// The parent decides whether the deadline is really the scope's own:
// context.WithTimeout keeps the earlier of the two deadlines, so deriving from a
// context that already carries one leaves the child on the parent's clock -- the
// shared-clock defect wearing the shape of a fix.
//
// The budget decides whether the phase runs on the number its documentation was
// written and measured against. Returned as a rendered label rather than judged
// here, so the caller can name the wrong budget in the words the author used.
func queryplanProfileDerivedCtx(assign *ast.AssignStmt) (name string, parent ast.Expr, budget string, ok bool) {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return "", nil, "", false
	}
	call, isCall := assign.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return "", nil, "", false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return "", nil, "", false
	}
	pkg, isIdent := selector.X.(*ast.Ident)
	if !isIdent || pkg.Name != "context" {
		return "", nil, "", false
	}
	if selector.Sel.Name != "WithTimeout" && selector.Sel.Name != "WithDeadline" {
		return "", nil, "", false
	}
	if len(call.Args) != 2 {
		return "", nil, "", false
	}
	target, isIdent := assign.Lhs[0].(*ast.Ident)
	if !isIdent {
		return "", nil, "", false
	}
	return target.Name, call.Args[0], queryplanProfileExprLabel(call.Args[1]), true
}

// queryplanProfileTestingIdents are the receivers whose Context() method is a
// deadline-free root here: *testing.T's context is cancelled when the test
// ends and carries no deadline of its own.
var queryplanProfileTestingIdents = map[string]bool{"t": true, "tb": true}

// queryplanProfileIsDeadlineFreeRoot reports whether an expression is a context
// root that carries no deadline: `t.Context()`, `context.Background()`, or
// `context.TODO()`.
//
// It fails closed. Anything else -- a variable, a field, a Context() call on
// some other receiver -- is treated as possibly deadline-carrying, because from
// the syntax alone it can be. Widening this set is a deliberate act; a rename
// that lands here makes the guard fail rather than quietly accept a parent it
// cannot vouch for.
func queryplanProfileIsDeadlineFreeRoot(parent ast.Expr) bool {
	call, ok := parent.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	if receiver.Name == "context" {
		return selector.Sel.Name == "Background" || selector.Sel.Name == "TODO"
	}
	return selector.Sel.Name == "Context" && queryplanProfileTestingIdents[receiver.Name]
}

// queryplanProfileExprLabel renders an expression the way it reads in source,
// for the shapes this guard reports on, so a failure names the parent or the
// budget the author actually wrote.
//
// Literals and arithmetic are rendered too, because a hardcoded `2*time.Minute`
// in a budget slot is one of the things the guard now rejects, and quoting it
// back is most of what makes that failure readable.
func queryplanProfileExprLabel(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.BasicLit:
		return typed.Value
	case *ast.SelectorExpr:
		return queryplanProfileExprLabel(typed.X) + "." + typed.Sel.Name
	case *ast.BinaryExpr:
		return queryplanProfileExprLabel(typed.X) + typed.Op.String() + queryplanProfileExprLabel(typed.Y)
	case *ast.CallExpr:
		return queryplanProfileExprLabel(typed.Fun) + "()"
	default:
		return "the expression it was given"
	}
}

// queryplanProfileGraphCallCtx reports the context expression handed to a graph
// driver call, and whether that call runs a profiled query.
//
// `session.Run(ctx, ...)` and `result.Consume(ctx)` run profiled queries.
// `driver.VerifyConnectivity(ctx)` does not, but it still spends a deadline
// against the graph, so its context is subject to the same rule and is reported
// with profiled=false. `t.Run(name, func(...))` is neither: it is matched by the
// closure argument and skipped.
//
// The context is returned as an expression rather than a variable name. It used
// to be returned as a name, and a call whose first argument was anything else
// was reported as no call at all -- so `session.Run(context.Background(), ...)`,
// a profiled query on no deadline whatsoever, was invisible to every assertion
// downstream. The caller now sees such a call and says so.
func queryplanProfileGraphCallCtx(call *ast.CallExpr) (ctx ast.Expr, profiled, ok bool) {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return nil, false, false
	}
	switch selector.Sel.Name {
	case "Run", "Consume":
		profiled = true
	case queryplanProfileConnectFunc:
	default:
		return nil, false, false
	}
	if len(call.Args) == 2 {
		if _, isSubtest := call.Args[1].(*ast.FuncLit); isSubtest {
			return nil, false, false
		}
	}
	if len(call.Args) == 0 {
		return nil, false, false
	}
	return call.Args[0], profiled, true
}

// queryplanProfileElapsedSinceVar reports the variable a `time.Since(x)`
// argument reads its elapsed time from.
func queryplanProfileElapsedSinceVar(arg ast.Expr) string {
	call, ok := arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Since" {
		return ""
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "time" {
		return ""
	}
	source, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return ""
	}
	return source.Name
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
		if scope.driverSite > 0 {
			scopes = append(scopes, scope)
		}
		return true
	})
	return scopes
}
