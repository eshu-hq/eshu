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
//
// The source reader this test asserts on lives in
// queryplan_profile_deadline_guard_ast_test.go.
const queryplanProfileLiveSourceFile = "queryplan_profile_live_test.go"

// queryplanProfileLiveTestFunc is the gate whose phases must not share a clock.
const queryplanProfileLiveTestFunc = "TestProductionQueryplanProfilesRejectWholeGraphScans"

// queryplanProfileSchemaFunc applies the gate's schema, including
// `CALL db.awaitIndexes(120)` -- the single statement that could spend the old
// shared 2-minute budget on its own.
const queryplanProfileSchemaFunc = "applyQueryplanProfileSchema"

// queryplanProfileConnectFunc is the driver call that opens the gate's first
// conversation with the graph. The wall-clock backstop has to be running by the
// time it is made, or the backstop does not cover the gate it claims to.
const queryplanProfileConnectFunc = "VerifyConnectivity"

// TestQueryplanProfileLiveGatePhasesOwnTheirDeadlines is the guard for the
// shared-deadline defect: the live gate used to build one 2-minute context and
// hand it to schema setup AND to all ~516 PROFILE subtests, so index population
// could spend the budget and hundreds of subtests then failed with the driver's
// "Timeout while waiting for connection" wording -- a message that blames the
// container for an exhausted clock.
//
// The property is structural, so this asserts it structurally, hermetically,
// and on every PR. Four things have to hold:
//
//   - every phase of the gate that talks to the graph derives its own deadline
//     in its own scope;
//   - that deadline comes from a parent carrying none, because
//     context.WithTimeout keeps the earlier of the two and a child of a
//     deadline-carrying context is still on the parent's clock;
//   - it is built from the named budget constant written for that phase, not a
//     literal duration and not a neighbouring phase's constant;
//   - the wall-clock backstop starts before the first graph call, so it covers
//     the gate it is documented to bound.
//
// Each was added because the previous set passed on the defect. The first alone
// passed on `context.WithTimeout(sharedCtx, queryplanProfileQueryBudget)`. The
// first two passed on `context.WithTimeout(t.Context(), 2*time.Minute)`, which
// is the coarse shared budget this change removed, restored per subtest while
// every guard stayed green.
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

	assertQueryplanProfileScopeOwnsDeadlines(t, queryplanProfileSchemaFunc, queryplanProfileSchemaBudgetName,
		queryplanProfileScopeOf(funcs[queryplanProfileSchemaFunc].Body))

	// The gate's own body runs the connect phase and nothing else against the
	// graph: profiled queries belong in subtests, where a failure names one
	// query instead of ending the run.
	liveScope := queryplanProfileScopeOf(funcs[queryplanProfileLiveTestFunc].Body)
	if liveScope.profileSite != 0 {
		t.Errorf("%s profiles %d quer(ies) in its own body; every profiled query belongs in a subtest that owns a deadline",
			queryplanProfileLiveTestFunc, liveScope.profileSite)
	}
	assertQueryplanProfileScopeOwnsDeadlines(t, queryplanProfileLiveTestFunc+" body",
		queryplanProfileConnectBudgetName, liveScope)

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
		assertQueryplanProfileScopeOwnsDeadlines(t, "subtest closure", queryplanProfileQueryBudgetName, scope)
	}
}

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

// assertQueryplanProfileScopeOwnsDeadlines fails when a scope hands the graph
// driver a context it did not give a deadline of its own -- the exact shape of
// the shared-clock defect -- or gives it one built from the wrong budget.
//
// wantBudget names the constant this phase's deadline must be built from.
func assertQueryplanProfileScopeOwnsDeadlines(t *testing.T, label, wantBudget string, scope queryplanProfileCtxScope) {
	t.Helper()
	if scope.driverSite == 0 {
		t.Errorf("%s hands the graph driver no context; this assertion would prove nothing", label)
		return
	}
	if len(scope.opaqueCtx) > 0 {
		sort.Strings(scope.opaqueCtx)
		t.Errorf("%s hands the graph driver context expression(s) %v rather than variables this scope gave a "+
			"deadline to. This reader used to drop such a call and assert on the rest, so a profiled query run on "+
			"context.Background() -- no deadline at all -- left nothing to fail on. Derive the deadline into a "+
			"variable with context.WithTimeout and pass that variable.", label, scope.opaqueCtx)
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
	assertQueryplanProfileScopeUsesItsBudget(t, label, wantBudget, scope)
}

// assertQueryplanProfileScopeUsesItsBudget fails when a phase builds its
// deadline from anything other than the budget constant written for it.
//
// The two checks above pin the shape -- own deadline, deadline-free parent --
// and TestQueryplanProfileBudgetsAreOrdered pins how the constants relate to
// each other. Between them sat the gap this closes: nothing read the second
// argument to context.WithTimeout, so a subtest could be handed a literal
// `2*time.Minute`, or the schema phase's 3 minutes, and every check stayed
// green while the per-query budget the measurements justify was gone.
//
// It fails closed both ways. An argument that is not one of the named budget
// constants is rejected on that ground, the way an unrecognised parent already
// is, and the count of budgets actually read is asserted so the loop cannot
// come back satisfied having looked at none.
func assertQueryplanProfileScopeUsesItsBudget(t *testing.T, label, wantBudget string, scope queryplanProfileCtxScope) {
	t.Helper()
	want, known := queryplanProfileBudgetConsts[wantBudget]
	if !known {
		t.Fatalf("%s: this guard asks for budget %q, which is not one of the live gate's budget constants %v; "+
			"it would reject every correct deadline", label, wantBudget, queryplanProfileBudgetNames())
	}
	names := make([]string, 0, len(scope.passed))
	for name := range scope.passed {
		names = append(names, name)
	}
	sort.Strings(names)
	checked := 0
	for _, name := range names {
		got, derivedHere := scope.budget[name]
		if !derivedHere {
			// Reported already: this scope did not give the context a deadline,
			// so there is no budget of its own to read.
			continue
		}
		checked++
		if got == wantBudget {
			continue
		}
		if _, isBudgetConst := queryplanProfileBudgetConsts[got]; !isBudgetConst {
			t.Errorf("%s builds %s's deadline from %s, which is not one of the live gate's budget constants %v. "+
				"A duration written inline is not covered by the measurements those constants carry and drifts "+
				"from them silently: use %s (%s).",
				label, name, got, queryplanProfileBudgetNames(), wantBudget, want)
			continue
		}
		t.Errorf("%s builds %s's deadline from %s, but this phase runs on %s. Each budget was sized against the "+
			"phase it names and they move independently -- %s is %s today, %s is %s -- so borrowing another "+
			"phase's number gives this one a limit nothing measured it against.",
			label, name, got, wantBudget, got, queryplanProfileBudgetConsts[got], wantBudget, want)
	}
	if checked == 0 {
		t.Errorf("%s: no context handed to the graph driver here was given a deadline in this scope, so the "+
			"%s check read nothing", label, wantBudget)
	}
}

// queryplanProfileBudgetNames lists the budget constants in a stable order, for
// failure messages that have to name the closed set.
func queryplanProfileBudgetNames() []string {
	names := make([]string, 0, len(queryplanProfileBudgetConsts))
	for name := range queryplanProfileBudgetConsts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
