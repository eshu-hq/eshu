// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

// The conditions Evaluate's eligibility switch tests (see doc_lockstep_test.go
// for the conventions the whole guard follows).
//
// Everything else in this package watches one of three things: the
// trigger-to-decision function over a sample, the shapes a Trigger may be
// written in, or the places a decision may be written. None of them watches
// what the existing gates *test*, and that is a distinct way to widen
// acceptance:
//
//	case !normalized.Enabled && normalized.Tool != "Terminal":
//
// With Enabled false and Tool "Terminal" that advises and publishes a
// 324-character advisory, for a hook whose entire premise is that it is opt-in.
// Every structural guard passes: the switch still has five clauses, all five
// still return skip, no new decision site appears, and no Trigger is written.
// The equivalence holds Tool at one value and the axis variants enumerate
// specific ones, so neither sees it either.
//
// This file pins the five conditions themselves. It is an enumeration, which
// this package is otherwise sceptical of, and the reason it is acceptable here
// is that the set is closed and already contractual: AGENTS.md names these five
// checks and their order as ADR-gated, so the list is a transcription of a
// contract rather than a guess at what someone might write. That is the same
// footing doc_lockstep_switch_test.go stands on for triggerAllowed's clauses.

// documentedEvaluateClauses is Evaluate's eligibility switch as AGENTS.md
// describes it: budget, then host, then enabled, then trigger, then permission,
// each testing exactly one thing about the normalized request. normalized is
// the name Evaluate bound normalizeInput's result to, so a rename moves the
// expectation with the code.
func documentedEvaluateClauses(normalized string) []string {
	return []string{
		normalized + ".Elapsed > " + normalized + ".Budget",
		normalized + ".Host != supportedHostClaude",
		"!" + normalized + ".Enabled",
		"!triggerAllowed(" + normalized + ".Trigger)",
		normalized + ".Permission == permissionDenied",
	}
}

// clauseText renders a case expression compactly enough to compare and to name
// in a failure. It is deliberately total: an operator it does not know renders
// as its node type rather than as something that might accidentally match.
func clauseText(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.BasicLit:
		return typed.Value
	case *ast.SelectorExpr:
		return clauseText(typed.X) + "." + typed.Sel.Name
	case *ast.UnaryExpr:
		return typed.Op.String() + clauseText(typed.X)
	case *ast.BinaryExpr:
		return clauseText(typed.X) + " " + typed.Op.String() + " " + clauseText(typed.Y)
	case *ast.ParenExpr:
		return "(" + clauseText(typed.X) + ")"
	case *ast.CallExpr:
		args := make([]string, 0, len(typed.Args))
		for _, arg := range typed.Args {
			args = append(args, clauseText(arg))
		}
		return clauseText(typed.Fun) + "(" + strings.Join(args, ", ") + ")"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// scanEvaluateClauses returns the variable funcName bound normalizeInput's
// result to and the rendered condition of every case clause in the switch at
// the top level of its body. A clause carrying several comma-separated
// expressions renders as all of them, so it cannot pass by matching on one.
func scanEvaluateClauses(dir, funcName string) (normalized string, clauses []string, err error) {
	target, err := findFuncDecl(dir, funcName)
	if err != nil {
		return "", nil, err
	}
	normalized, _ = normalizedInputVar(target)
	for _, stmt := range target.Body.List {
		switchStmt, isSwitch := stmt.(*ast.SwitchStmt)
		if !isSwitch || switchStmt.Body == nil {
			continue
		}
		for _, item := range switchStmt.Body.List {
			clause, isClause := item.(*ast.CaseClause)
			if !isClause {
				continue
			}
			if len(clause.List) == 0 {
				clauses = append(clauses, "default")
				continue
			}
			rendered := make([]string, 0, len(clause.List))
			for _, expr := range clause.List {
				rendered = append(rendered, clauseText(expr))
			}
			clauses = append(clauses, strings.Join(rendered, ", "))
		}
	}
	return normalized, clauses, nil
}

// TestDocLockstepEvaluateClausesTestWhatTheyDocument pins each eligibility
// clause to the single condition AGENTS.md says it tests, so an extra conjunct
// -- the one way left to widen acceptance without touching the trigger, adding
// a decision site, or changing the clause count -- is a finding.
func TestDocLockstepEvaluateClausesTestWhatTheyDocument(t *testing.T) {
	t.Parallel()

	// The behavioural control. A request ineligible on exactly one clause must
	// skip, or the clause comparison below is pinning conditions that decide
	// nothing.
	disabled := advisableInput()
	disabled.Enabled = false
	if out := Evaluate(disabled); out.Decision != decisionSkip || out.Reason != reasonDisabled {
		t.Fatalf("a request with Enabled=false came back %q/%q, want skip/%s; the clause pins below would be describing a switch that does not gate", out.Decision, out.Reason, reasonDisabled)
	}

	normalized, clauses, err := scanEvaluateClauses(".", "Evaluate")
	if err != nil {
		t.Fatalf("scan Evaluate's switch: %v", err)
	}
	if normalized == "" {
		t.Fatal("Evaluate does not bind normalizeInput's result to exactly one variable; the clauses below have no normalized value to be held to")
	}
	if len(clauses) == 0 {
		t.Fatal("found no case clauses in a switch at the top level of Evaluate's body; the assertion would be vacuous")
	}

	want := documentedEvaluateClauses(normalized)
	if len(clauses) != len(want) {
		t.Fatalf("Evaluate's switch has %d clauses %v, want %d %v", len(clauses), clauses, len(want), want)
	}
	for i, got := range clauses {
		if got == want[i] {
			continue
		}
		t.Errorf("clause %d tests `%s`, want `%s`; a clause that tests more than the one condition it documents widens or narrows acceptance while the clause count, the fail-open rule and every trigger guard stay satisfied",
			i+1, got, want[i])
	}
}
