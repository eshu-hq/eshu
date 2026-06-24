// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import "testing"

// TestLowerIfRecordsGuardPredicate proves Go lowering keeps the branch
// predicate text as control-dependence provenance without graph persistence.
func TestLowerIfRecordsGuardPredicate(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(req string, allowed bool) {
	if allowed {
		sink(req)
	}
}
`
	fn := lowerFirstFunction(t, src)
	if len(fn.ControlDependencies) != 1 {
		t.Fatalf("control dependencies = %+v, want one guarded sink block", fn.ControlDependencies)
	}
	if got := fn.ControlDependencies[0].Guard; got != "allowed" {
		t.Fatalf("guard = %q, want allowed", got)
	}
}

// TestLowerIfGuardPredicateRedactsLiterals proves guard provenance keeps the
// predicate shape without carrying source literal values.
func TestLowerIfGuardPredicateRedactsLiterals(t *testing.T) {
	t.Parallel()

	src := "package main\n\nfunc handler(token string, retries int) {\n\tif token == \"secret-token\" && retries > 3 {\n\t\tsink(token)\n\t}\n}\n"
	fn := lowerFirstFunction(t, src)
	if len(fn.ControlDependencies) != 1 {
		t.Fatalf("control dependencies = %+v, want one guarded sink block", fn.ControlDependencies)
	}
	if got, want := fn.ControlDependencies[0].Guard, "token == <literal> && retries > <literal>"; got != want {
		t.Fatalf("guard = %q, want %q", got, want)
	}
}

// TestLowerIfEarlyReturnReportsFallthroughPredicate proves false/fallthrough
// dependencies carry the predicate that makes the guarded sink reachable.
func TestLowerIfEarlyReturnReportsFallthroughPredicate(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(req string, allowed bool) {
	if !allowed {
		return
	}
	sink(req)
}
`
	fn := lowerFirstFunction(t, src)
	if len(fn.ControlDependencies) != 2 {
		t.Fatalf("control dependencies = %+v, want return and fallthrough dependencies", fn.ControlDependencies)
	}
	sinkBlock := -1
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			if stmt.Line == 7 {
				sinkBlock = block.ID
			}
		}
	}
	if sinkBlock < 0 {
		t.Fatalf("sink block not found in blocks: %+v", fn.Blocks)
	}
	sinkGuard := ""
	for _, dep := range fn.ControlDependencies {
		if dep.DependentBlock == sinkBlock {
			sinkGuard = dep.Guard
		}
	}
	if sinkGuard != "allowed" {
		t.Fatalf("sink guard = %q, want allowed; deps=%+v", sinkGuard, fn.ControlDependencies)
	}
}

func TestGoNegatedGuardTextPreservesCompoundNegation(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"!allowed":                   "allowed",
		"!(allowed)":                 "allowed",
		"!(token == <literal>)":      "token == <literal>",
		"!allowed || admin":          "!(!allowed || admin)",
		"!(allowed) || admin":        "!(!(allowed) || admin)",
		"!(token == <literal>) || x": "!(!(token == <literal>) || x)",
	}
	for input, want := range tests {
		if got := goNegatedGuardText(input); got != want {
			t.Fatalf("goNegatedGuardText(%q) = %q, want %q", input, got, want)
		}
	}
}
