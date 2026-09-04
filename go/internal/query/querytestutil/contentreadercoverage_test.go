// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// answeringBranchCounts reports how many rows-answering branches each
// default-answer helper has, read from the source rather than from a constant.
//
// A branch is a `return &contentReaderRows{...}` inside one of the two group
// functions. Counting predicates instead would be wrong: several branches AND
// two to four strings.Contains calls together, so a predicate count reads high
// and would make an incomplete table look covered.
func answeringBranchCounts(t *testing.T) map[string]int {
	t.Helper()

	const source = "contentreaderdefaults.go"

	file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}

	counts := map[string]int{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name != "contentReaderFactDefaultRows" && fn.Name.Name != "contentReaderReadModelDefaultRows" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			ret, ok := node.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			unary, ok := ret.Results[0].(*ast.UnaryExpr)
			if !ok || unary.Op != token.AND {
				return true
			}
			composite, ok := unary.X.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if ident, ok := composite.Type.(*ast.Ident); ok && ident.Name == "contentReaderRows" {
				counts[fn.Name.Name]++
			}
			return true
		})
	}
	return counts
}

// TestContentReaderDefaultGroupsCoverEveryGroupAnsweringBranch requires
// defaultRowsCases to hold one case per answering branch, per GROUP.
//
// Group, not every branch in the file. contentReaderDefaultRows has its own
// answering branches that run before it delegates to either group, and they are
// a different tier: they answer the incidental reads both groups are indifferent
// to, so they have no group to be disjoint from and no place in this corpus.
// TestContentReaderCommonDispatchDoesNotPreemptGroupQueries guards the one way
// that tier can go wrong.
//
// Without this, the disjointness and shape tests both walk defaultRowsCases and
// pass no matter how short it is, so a branch added to either helper without a
// matching case is silently unguarded -- and the split of these defaults into
// per-group files is safe only while every branch is known to belong to exactly
// one group. This reads the branch count out of the source so that adding a
// branch fails here until someone adds the case, rather than being compared
// against a hand-maintained number that a rebase can quietly merge away.
func TestContentReaderDefaultGroupsCoverEveryGroupAnsweringBranch(t *testing.T) {
	t.Parallel()

	counts := answeringBranchCounts(t)
	if len(counts) != 2 {
		t.Fatalf("found %d default-answer helpers, want 2: %v", len(counts), counts)
	}

	covered := map[defaultRowsGroup]int{}
	for _, testCase := range defaultRowsCases() {
		covered[testCase.group]++
	}

	for _, check := range []struct {
		fn    string
		group defaultRowsGroup
		label string
	}{
		{fn: "contentReaderFactDefaultRows", group: factGroup, label: "fact"},
		{fn: "contentReaderReadModelDefaultRows", group: readModelGroup, label: "read-model"},
	} {
		branches := counts[check.fn]
		if branches == 0 {
			t.Fatalf("%s: no answering branches found; did %s move or get renamed?", check.label, check.fn)
		}
		if got := covered[check.group]; got != branches {
			t.Errorf(
				"%s group: %d cases in defaultRowsCases for %d answering branches in %s; add one representative query per branch",
				check.label, got, branches, check.fn,
			)
		}
	}
}

// TestContentReaderCommonDispatchDoesNotPreemptGroupQueries requires the shared
// tier in contentReaderDefaultRows to leave every group-owned query to its group.
//
// That tier runs first, so a branch added there whose predicate is loose enough
// to catch a group's query wins by evaluation order alone. Nothing else notices:
// the disjointness test calls the two group helpers directly and never goes
// through the dispatcher, so it would keep passing while real callers silently
// got the shared tier's answer instead of the group's.
//
// Checking the dispatcher against the owning group for every case in the corpus
// catches that without a second corpus for the shared tier -- the failure mode
// worth guarding is preemption of a known group query, not the shared branches
// answering their own reads.
func TestContentReaderCommonDispatchDoesNotPreemptGroupQueries(t *testing.T) {
	t.Parallel()

	for _, testCase := range defaultRowsCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			owner := contentReaderFactDefaultRows(testCase.query, nil)
			if testCase.group == readModelGroup {
				owner = contentReaderReadModelDefaultRows(testCase.query, nil)
			}
			if owner == nil {
				t.Fatalf("owning group did not answer %q; defaultRowsCases is wrong", testCase.query)
			}

			dispatched := contentReaderDefaultRows(testCase.query, nil)
			if dispatched == nil {
				t.Fatalf("dispatcher answered nothing for %q while its group answered", testCase.query)
			}

			wantColumns := owner.Columns()
			gotColumns := dispatched.Columns()
			if len(gotColumns) != len(wantColumns) {
				t.Fatalf(
					"dispatcher answered %q with columns %v, group answers %v; a shared-tier branch is preempting this group query",
					testCase.query, gotColumns, wantColumns,
				)
			}
			for i := range wantColumns {
				if gotColumns[i] != wantColumns[i] {
					t.Fatalf(
						"dispatcher answered %q with columns %v, group answers %v; a shared-tier branch is preempting this group query",
						testCase.query, gotColumns, wantColumns,
					)
				}
			}
		})
	}
}
