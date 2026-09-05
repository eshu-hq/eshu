// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// highestPlaceholder returns the largest $N in query, which is the number of
// arguments the statement binds.
func highestPlaceholder(t *testing.T, query string) int {
	t.Helper()
	highest := 0
	for _, match := range placeholderPattern.FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse placeholder %q: %v", match[0], err)
		}
		if n > highest {
			highest = n
		}
	}
	if highest == 0 {
		t.Fatal("no $N placeholders found; the query text or this pattern changed")
	}
	return highest
}

// TestSupplyChainRuntimeFilterListArgsMatchQueryPlaceholders keeps the EXPLAIN
// proof's argument list in step with the queries it binds.
//
// supplyChainRuntimeFilterListArgs drifted once already: a $24::timestamptz was
// added for suppression-expiry evaluation and the helper kept building 23. The
// only thing that caught it was TestSupplyChainImpactRuntimeFilterPlansLive,
// which skips without ESHU_POSTGRES_TEST_DSN and so never runs in ordinary CI --
// and when it did run it failed at bind time with a parameter-count message,
// before producing a plan, so every index assertion it exists for was skipped.
// A test that fails for the wrong reason still looks like signal while hiding
// the absence of its own coverage.
//
// The expected count is DERIVED from the query rather than written here as a
// constant. A hardcoded number only catches the case where someone updates the
// query and the production args but forgets this helper; it passes happily if
// they forget the constant too, which is the same drift one level up. Reading
// the highest $N means adding a placeholder anywhere fails this test until the
// helper is updated with it.
//
// Both queries are checked because they take the SAME slice
// (supply_chain_impact_findings_queries.go:129), so a placeholder added to
// either one alone is a real defect.
func TestSupplyChainRuntimeFilterListArgsMatchQueryPlaceholders(t *testing.T) {
	t.Parallel()

	args := supplyChainRuntimeFilterListArgs(impact.SupplyChainImpactFindingFilter{})

	for name, query := range map[string]string{
		"list direct":       impact.ListSupplyChainImpactFindingsQuery,
		"list materialized": impact.ListSupplyChainImpactFindingsFromWinnersQuery,
	} {
		t.Run(name, func(t *testing.T) {
			want := highestPlaceholder(t, query)
			if len(args) != want {
				t.Fatalf(
					"supplyChainRuntimeFilterListArgs binds %d arguments, but %s uses $%d; "+
						"a placeholder was added to the query without adding its argument here, "+
						"which fails the live plan proof at bind time before any plan is produced",
					len(args), name, want,
				)
			}
		})
	}
}

// productionQueryArgCount returns how many arguments the QueryContext call
// inside fnName passes after ctx and the query, read from the source rather
// than executed.
//
// It is read statically because the production list is built inline in the call
// expression, so there is no value to measure at run time without executing the
// query -- which needs a database, which is what put this drift beyond CI's
// reach in the first place.
func productionQueryArgCount(t *testing.T, file, fnName string) int {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	count := -1
	// found counts every QueryContext call, not just the last one. ast.Inspect's
	// `return false` prunes only the matched call's own subtree; the walk
	// continues past it. Without this count a second QueryContext added to the
	// function later would overwrite count and the guard would assert the wrong
	// call -- a false green whenever that call happened to take the same number
	// of arguments.
	found := 0
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != fnName {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "QueryContext" {
				return true
			}
			// minus ctx and the query itself
			count = len(call.Args) - 2
			found++
			return false
		})
	}
	if found == 0 {
		t.Fatalf("no QueryContext call found in %s of %s; if it was renamed or the query moved, update this test with it", fnName, file)
	}
	if found != 1 {
		t.Fatalf("expected exactly one QueryContext call in %s of %s, found %d; this guard binds one query to one argument list, so split the assertion per call before adding another", fnName, file, found)
	}
	return count
}

// TestListSupplyChainImpactFindingsBindsEveryPlaceholder guards the PRODUCTION
// argument list, not just the EXPLAIN helper.
//
// The sibling test above ties supplyChainRuntimeFilterListArgs to the query, but
// that helper only feeds the live plan proof. ListSupplyChainImpactFindings
// builds its own list inline, and no ordinary test checks it: the tests that
// call it use recording or failing fakes that return before the query executes,
// so the only thing that binds the real list is a DSN-gated live run.
//
// A placeholder added to the query and mirrored into the helper alone would
// therefore leave production broken and green in CI -- the same silent drift
// this pair of tests exists to remove, one file over.
func TestListSupplyChainImpactFindingsBindsEveryPlaceholder(t *testing.T) {
	t.Parallel()

	got := productionQueryArgCount(t, "impact/supply_chain_impact_findings.go", "ListSupplyChainImpactFindings")

	for name, query := range map[string]string{
		"list direct":       impact.ListSupplyChainImpactFindingsQuery,
		"list materialized": impact.ListSupplyChainImpactFindingsFromWinnersQuery,
	} {
		t.Run(name, func(t *testing.T) {
			want := highestPlaceholder(t, query)
			if got != want {
				t.Fatalf(
					"ListSupplyChainImpactFindings passes %d arguments to QueryContext, but %s uses $%d; "+
						"the production list and the query drifted apart, which only a live run would catch",
					got, name, want,
				)
			}
		})
	}
}
