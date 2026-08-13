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
// contexts: which context variables it derives a deadline for from a clock-free
// parent, which it derives from a parent that may already be running down a
// clock, and which it hands to the graph driver.
type queryplanProfileCtxScope struct {
	derived map[string]bool
	// chained maps a context variable to the parent it took its deadline from,
	// when that parent is not a deadline-free root. context.WithTimeout keeps
	// whichever deadline is EARLIER, so such a context is still on the parent's
	// clock no matter what budget it was given.
	chained map[string]string
	passed  map[string]bool
	// profileSite counts calls that run a profiled query (Run, Consume). Those
	// belong in a subtest, never in the gate's own body.
	profileSite int
	// driverSite counts every call that hands the driver a context, profiled
	// query or not. VerifyConnectivity is one: the connect phase talks to the
	// graph on a deadline like any other phase, so its context is held to the
	// same rule.
	driverSite int
}

// queryplanProfileScopeOf reads one function body's context use, without
// descending into nested closures: a subtest closure's deadlines are that
// closure's business, and the enclosing body must not be credited with them.
func queryplanProfileScopeOf(body *ast.BlockStmt) queryplanProfileCtxScope {
	scope := queryplanProfileCtxScope{
		derived: map[string]bool{},
		chained: map[string]string{},
		passed:  map[string]bool{},
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			name, parent, ok := queryplanProfileDerivedCtxName(typed)
			switch {
			case !ok:
			case queryplanProfileIsDeadlineFreeRoot(parent):
				scope.derived[name] = true
			default:
				scope.chained[name] = queryplanProfileExprLabel(parent)
			}
		case *ast.CallExpr:
			name, profiled, ok := queryplanProfileGraphCallCtxName(typed)
			if ok {
				scope.passed[name] = true
				scope.driverSite++
				if profiled {
					scope.profileSite++
				}
			}
		}
		return true
	})
	return scope
}

// queryplanProfileDerivedCtxName reports the variable an assignment gives a
// deadline to, and the parent that deadline was derived from, as in
// `ctx, cancel := context.WithTimeout(parent, budget)`.
//
// The parent is returned rather than discarded because it decides whether the
// deadline is really the scope's own. context.WithTimeout keeps the earlier of
// the two deadlines, so deriving from a context that already carries one leaves
// the child on the parent's clock -- the shared-clock defect wearing the shape
// of a fix.
func queryplanProfileDerivedCtxName(assign *ast.AssignStmt) (string, ast.Expr, bool) {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return "", nil, false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return "", nil, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "context" {
		return "", nil, false
	}
	if selector.Sel.Name != "WithTimeout" && selector.Sel.Name != "WithDeadline" {
		return "", nil, false
	}
	if len(call.Args) != 2 {
		return "", nil, false
	}
	target, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	return target.Name, call.Args[0], true
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
// for the shapes this guard reports on, so a failure names the parent the
// author actually wrote.
func queryplanProfileExprLabel(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return queryplanProfileExprLabel(typed.X) + "." + typed.Sel.Name
	case *ast.CallExpr:
		return queryplanProfileExprLabel(typed.Fun) + "()"
	default:
		return "the expression it was given"
	}
}

// queryplanProfileGraphCallCtxName reports the context variable handed to a
// graph driver call, and whether that call runs a profiled query.
//
// `session.Run(ctx, ...)` and `result.Consume(ctx)` run profiled queries.
// `driver.VerifyConnectivity(ctx)` does not, but it still spends a deadline
// against the graph, so its context is subject to the same rule and is reported
// with profiled=false. `t.Run(name, func(...))` is neither: it is matched by the
// closure argument and skipped.
func queryplanProfileGraphCallCtxName(call *ast.CallExpr) (name string, profiled, ok bool) {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return "", false, false
	}
	switch selector.Sel.Name {
	case "Run", "Consume":
		profiled = true
	case queryplanProfileConnectFunc:
	default:
		return "", false, false
	}
	if len(call.Args) == 2 {
		if _, isSubtest := call.Args[1].(*ast.FuncLit); isSubtest {
			return "", false, false
		}
	}
	if len(call.Args) == 0 {
		return "", false, false
	}
	ctx, isIdent := call.Args[0].(*ast.Ident)
	if !isIdent {
		return "", false, false
	}
	return ctx.Name, profiled, true
}

// TestQueryplanProfileLiveGatePhasesOwnTheirDeadlines is the guard for the
// shared-deadline defect: the live gate used to build one 2-minute context and
// hand it to schema setup AND to all ~516 PROFILE subtests, so index population
// could spend the budget and hundreds of subtests then failed with the driver's
// "Timeout while waiting for connection" wording -- a message that blames the
// container for an exhausted clock.
//
// The property is structural, so this asserts it structurally, hermetically,
// and on every PR. Three things have to hold:
//
//   - every phase of the gate that talks to the graph derives its own deadline
//     in its own scope;
//   - that deadline comes from a parent carrying none, because
//     context.WithTimeout keeps the earlier of the two and a child of a
//     deadline-carrying context is still on the parent's clock;
//   - the wall-clock backstop starts before the first graph call, so it covers
//     the gate it is documented to bound.
//
// The first check alone used to pass on
// `context.WithTimeout(sharedCtx, queryplanProfileQueryBudget)`, which is the
// defect back in the door the guard was built to close.
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

	// The gate's own body runs the connect phase and nothing else against the
	// graph: profiled queries belong in subtests, where a failure names one
	// query instead of ending the run.
	liveScope := queryplanProfileScopeOf(funcs[queryplanProfileLiveTestFunc].Body)
	if liveScope.profileSite != 0 {
		t.Errorf("%s profiles %d quer(ies) in its own body; every profiled query belongs in a subtest that owns a deadline",
			queryplanProfileLiveTestFunc, liveScope.profileSite)
	}
	assertQueryplanProfileScopeOwnsDeadlines(t, queryplanProfileLiveTestFunc+" body", liveScope)

	assertQueryplanProfileBackstopStartsWithTheGate(t, funcs[queryplanProfileLiveTestFunc])

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

// queryplanProfileConnectFunc is the driver call that opens the gate's first
// conversation with the graph. The wall-clock backstop has to be running by the
// time it is made, or the backstop does not cover the gate it claims to.
const queryplanProfileConnectFunc = "VerifyConnectivity"

// assertQueryplanProfileBackstopStartsWithTheGate fails when the clock read by
// queryplanProfileTotalBudgetError starts after the gate has already begun
// talking to the graph.
//
// queryplanProfileTotalBudget is documented as the backstop for the WHOLE gate
// and was sized against whole-test runtimes. It used to be measured from a
// time.Now() taken after connect and schema had both finished, so the two
// phases that can cost minutes -- `db.awaitIndexes(120)` above all -- fell
// outside the number meant to bound them. That gap is invisible at review time
// and reappears with any edit that moves the clock down, so it is pinned here.
func assertQueryplanProfileBackstopStartsWithTheGate(t *testing.T, fn *ast.FuncDecl) {
	t.Helper()
	var clockVar string
	var clockStart, connect token.Pos
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "queryplanProfileTotalBudgetError" &&
			len(call.Args) > 0 && clockVar == "" {
			clockVar = queryplanProfileElapsedSinceVar(call.Args[0])
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok &&
			selector.Sel.Name == queryplanProfileConnectFunc && !connect.IsValid() {
			connect = call.Pos()
		}
		return true
	})
	if clockVar == "" {
		t.Fatalf("%s no longer measures its wall-clock backstop with queryplanProfileTotalBudgetError(time.Since(...)); "+
			"this assertion would prove nothing -- point it at the new shape", queryplanProfileLiveTestFunc)
	}
	if !connect.IsValid() {
		t.Fatalf("%s no longer calls %s; this assertion would prove nothing -- point it at the call that now opens "+
			"the gate's first graph conversation", queryplanProfileLiveTestFunc, queryplanProfileConnectFunc)
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || clockStart.IsValid() {
			return true
		}
		target, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || target.Name != clockVar {
			return true
		}
		if queryplanProfileExprLabel(assign.Rhs[0]) != "time.Now()" {
			return true
		}
		clockStart = assign.Pos()
		return true
	})
	if !clockStart.IsValid() {
		t.Fatalf("%s never starts %s with time.Now(); the wall-clock backstop reads a clock this guard cannot locate",
			queryplanProfileLiveTestFunc, clockVar)
	}
	if clockStart > connect {
		t.Errorf("%s starts its wall-clock backstop (%s) after it has already called %s. "+
			"queryplanProfileTotalBudget is documented as the backstop for the whole gate and sized against "+
			"whole-test runtimes, so connect and schema must sit inside it -- the schema phase alone is allowed "+
			"3 minutes. Start the clock before the first graph call.",
			queryplanProfileLiveTestFunc, clockVar, queryplanProfileConnectFunc)
	}
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

// assertQueryplanProfileScopeOwnsDeadlines fails when a scope hands the graph
// driver a context it did not give a deadline of its own -- the exact shape of
// the shared-clock defect.
func assertQueryplanProfileScopeOwnsDeadlines(t *testing.T, label string, scope queryplanProfileCtxScope) {
	t.Helper()
	if scope.driverSite == 0 {
		t.Errorf("%s hands the graph driver no context; this assertion would prove nothing", label)
		return
	}
	borrowed := make([]string, 0, len(scope.passed))
	chained := make([]string, 0, len(scope.passed))
	for name := range scope.passed {
		switch {
		case scope.derived[name]:
		case scope.chained[name] != "":
			chained = append(chained, name+" (from "+scope.chained[name]+")")
		default:
			borrowed = append(borrowed, name)
		}
	}
	sort.Strings(borrowed)
	sort.Strings(chained)
	if len(borrowed) > 0 {
		t.Errorf("%s hands the graph driver context(s) %v that it did not give a deadline of its own. "+
			"A context borrowed from an enclosing scope shares that scope's clock: when it expires, every "+
			"remaining call fails at once with the driver's connectivity wording instead of one honest "+
			"deadline message. Derive the deadline here with context.WithTimeout.", label, borrowed)
	}
	if len(chained) > 0 {
		t.Errorf("%s derives context(s) %v from a parent that may already be counting down. "+
			"context.WithTimeout keeps the EARLIER deadline, so this looks like a per-scope budget while the "+
			"scope still dies on the enclosing clock -- the shared-deadline defect with a fix's shape. Derive "+
			"from a deadline-free root instead: t.Context(), context.Background(), or context.TODO().",
			label, chained)
	}
}
