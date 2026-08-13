// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Structural half of the trigger-class pin (see doc_lockstep_test.go for the
// conventions the whole guard follows).
//
// The first version of this scanner collected the string literals of any case
// clause whose body held a bare `return true`. That reads the accepted set out
// of the code, which is the thing a hand-written probe list cannot do -- but it
// still recognizes exactly one way to write the function. Four rewrites of
// triggerAllowed that accept a new class all passed the entire suite: an early
// `if trigger == "list" { return true }` before the switch, an `if` guard
// inside a case clause, a case clause that returns a local variable, and a case
// clause that returns a comparison. Every one of them made
// triggerAllowed("list") report true.
//
// So the scanner asserts the shape of the function rather than pattern-matching
// one spelling of it. triggerAllowed's body must be exactly one switch, tagged
// with its own sole parameter, whose clauses are string-literal cases returning
// a bare `true`, plus one default returning a bare `false`. A statement before
// or after the switch, a conditional or an assignment inside a clause, a
// returned variable or expression, a tagless switch, a missing default, a
// default that returns true, or a case that returns anything but `true` are all
// findings. That turns each evasion above into a structural violation instead
// of an invisible one.
//
// The tag rule came a round later, for the same reason. Checking only that a
// tag existed left `switch canonicalTrigger(trigger)` -- with a helper mapping
// "list" onto "read" -- reading as a perfectly closed switch. The tag may now
// be wrapped in strings.TrimSpace and strings.ToLower and nothing else: those
// fold spellings of one class together and cannot turn one class into another,
// which keeps a legitimate hardening of triggerAllowed inside this function
// instead of pushing it into normalizeInput, where it becomes the remap
// doc_lockstep_trigger_path_test.go refuses.
//
// The fixture drive for all of it lives in doc_lockstep_switch_fixtures_test.go.

// switchFinding is one departure from the closed-switch shape, named so a
// failure says which rule the function broke rather than reporting a bare
// mismatch.
type switchFinding struct {
	Func   string
	Detail string
}

func (f switchFinding) String() string { return f.Func + ": " + f.Detail }

// findFuncDecl returns the package-level function named name declared in one of
// dir's non-test files. It errors when there is none -- an empty result would
// otherwise read to a caller as "this function accepts nothing" -- and errors
// again when two files declare it, because the old version returned the first
// match in sorted filename order and had no way to say which one the compiler
// uses. Two declarations of the same name only compile when one file is
// build-excluded, which parseNonTestGoFiles now drops and
// TestDocLockstepNoBuildConstrainedFiles fails on; this is the belt for that
// brace.
func findFuncDecl(dir, name string) (*ast.FuncDecl, error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return nil, err
	}
	fileNames := make([]string, 0, len(parsed))
	for fileName := range parsed {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)
	var found *ast.FuncDecl
	var declaredIn []string
	for _, fileName := range fileNames {
		for _, decl := range parsed[fileName].Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if ok && funcDecl.Recv == nil && funcDecl.Name.Name == name {
				if found == nil {
					found = funcDecl
				}
				declaredIn = append(declaredIn, fileName)
			}
		}
	}
	if len(declaredIn) > 1 {
		return nil, fmt.Errorf("func %s is declared in %d files (%s) in %s; the scanner cannot tell which one the compiler uses", name, len(declaredIn), strings.Join(declaredIn, ", "), dir)
	}
	if found == nil {
		return nil, fmt.Errorf("func %s not found in %s", name, dir)
	}
	return found, nil
}

// soleParamName reports the name of fn's single parameter, and false when fn
// takes anything other than exactly one named parameter. The switch tag is held
// to this name, so a function with no parameter to compare against has no shape
// to check.
func soleParamName(fn *ast.FuncDecl) (string, bool) {
	if fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return "", false
	}
	field := fn.Type.Params.List[0]
	if len(field.Names) != 1 {
		return "", false
	}
	return field.Names[0].Name, true
}

// pureTriggerNormalizers are the strings functions a value may pass through on
// its way into the switch tag. Both fold representations of the same class
// together -- case and surrounding whitespace -- and neither can turn one class
// into another, which is the property that matters: a tag of
// `strings.ToLower(strings.TrimSpace(trigger))` still answers for exactly the
// literals the cases name, while `canonicalTrigger(trigger)` can map "list"
// onto "read". That distinction is why the tag check allows the first and
// refuses the second, and it is where a legitimate hardening of triggerAllowed
// belongs -- moving it into normalizeInput instead is the remap
// TestDocLockstepTriggerReachesTheGateUnrewritten refuses.
func pureTriggerNormalizers() map[string]bool {
	return map[string]bool{"ToLower": true, "TrimSpace": true}
}

// unwrapPureNormalizers peels calls to pureTriggerNormalizers off expr and
// returns what is underneath, reporting false as soon as it meets a call that
// is anything else.
func unwrapPureNormalizers(expr ast.Expr) (ast.Expr, bool) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return expr, true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) != 1 {
			return expr, false
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "strings" || !pureTriggerNormalizers()[selector.Sel.Name] {
			return expr, false
		}
		expr = call.Args[0]
	}
}

// identAfterPureNormalizers reports the identifier expr resolves to once any
// pure normalizer wrappers are removed.
func identAfterPureNormalizers(expr ast.Expr) (string, bool) {
	root, pure := unwrapPureNormalizers(expr)
	if !pure {
		return "", false
	}
	ident, ok := root.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// bareReturnIdent reports the identifier a clause body returns, when that body
// is exactly one `return <ident>` and nothing else. Anything longer or anything
// returning an expression fails here, which is what stops a conditional, an
// assignment, or a comparison from standing in for the literal answer.
func bareReturnIdent(body []ast.Stmt) (string, bool) {
	if len(body) != 1 {
		return "", false
	}
	ret, ok := body[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// scanClosedStringSwitch requires funcName in dir to be a closed string switch
// and returns the literals its true-returning clauses carry, how many such
// clauses it saw, and every departure from that shape. It reports findings
// rather than failing so the negative tests below can drive the same code over
// fixture directories.
func scanClosedStringSwitch(dir, funcName string) (clauses int, values []string, findings []switchFinding, err error) {
	target, err := findFuncDecl(dir, funcName)
	if err != nil {
		return 0, nil, nil, err
	}
	report := func(format string, args ...any) {
		findings = append(findings, switchFinding{Func: funcName, Detail: fmt.Sprintf(format, args...)})
	}

	statements := 0
	if target.Body != nil {
		statements = len(target.Body.List)
	}
	if statements != 1 {
		report("body holds %d statements, want exactly one switch; a statement before or after the switch can accept a class the switch never names", statements)
		return 0, nil, findings, nil
	}
	switchStmt, ok := target.Body.List[0].(*ast.SwitchStmt)
	if !ok {
		report("the body statement is %T, want a switch", target.Body.List[0])
		return 0, nil, findings, nil
	}
	if switchStmt.Init != nil {
		report("the switch carries an init statement, which can settle the answer before any case is compared")
	}
	param, hasParam := soleParamName(target)
	switch {
	case !hasParam:
		report("the function does not take exactly one named parameter, so there is no value to hold the switch tag to")
	case switchStmt.Tag == nil:
		report("the switch has no tag, so its cases are arbitrary boolean expressions rather than string literals")
	default:
		if name, ok := identAfterPureNormalizers(switchStmt.Tag); !ok || name != param {
			report("the switch tag is not the bare parameter %q (optionally wrapped in strings.TrimSpace/ToLower), so the compared value can be rewritten before any case is reached", param)
		}
	}
	if switchStmt.Body == nil {
		report("the switch has no body")
		return 0, nil, findings, nil
	}

	defaults := 0
	for _, stmt := range switchStmt.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			report("the switch holds a %T, want only case clauses", stmt)
			continue
		}
		if clause.List == nil {
			defaults++
			if name, ok := bareReturnIdent(clause.Body); !ok || name != "false" {
				report("the default clause is not a bare `return false`")
			}
			continue
		}
		literals := make([]string, 0, len(clause.List))
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				report("a case compares a %T rather than a string literal", expr)
				continue
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return clauses, values, findings, unquoteErr
			}
			literals = append(literals, value)
		}
		if name, ok := bareReturnIdent(clause.Body); !ok || name != "true" {
			report("case %v is not a bare `return true`", literals)
			continue
		}
		clauses++
		values = append(values, literals...)
	}
	if defaults != 1 {
		report("the switch has %d default clauses, want exactly one returning false", defaults)
	}
	sort.Strings(values)
	return clauses, values, findings, nil
}
