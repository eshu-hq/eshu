// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"database/sql/driver"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"strings"
	"testing"
)

// groupHelperSuffix is the naming convention every per-group default-answer
// helper follows. Discovery is by suffix rather than by a list of known names so
// that a third group helper is FOUND and reported, not silently skipped: a test
// that hardcodes the two names it expects cannot notice a third.
const groupHelperSuffix = "DefaultRows"

// dispatcherName is the shared tier, which delegates to the group helpers and is
// therefore not one of them.
const dispatcherName = "contentReaderDefaultRows"

// answeringBranchCounts reports how many rows-answering branches each per-group
// default-answer helper has, read from the source rather than from a constant.
//
// A branch is a `return &contentReaderRows{...}`. Counting predicates instead
// would be wrong: several branches AND two to four strings.Contains calls
// together, so a predicate count reads high and would make an incomplete table
// look covered.
//
// Every other return in a group helper must be a bare `return nil`. Without that
// rule a branch could answer through a helper call or a local variable and never
// be counted, which is the same false green as an uncounted literal. The shared
// tier is excluded because it legitimately returns the delegated result.
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
		name := fn.Name.Name
		if name == dispatcherName || !strings.HasSuffix(name, groupHelperSuffix) {
			continue
		}

		counts[name] = 0
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			ret, ok := node.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			if answersWithRowsLiteral(ret.Results[0]) {
				counts[name]++
				return true
			}
			if ident, ok := ret.Results[0].(*ast.Ident); ok && ident.Name == "nil" {
				return true
			}
			t.Errorf(
				"%s returns something that is neither a &contentReaderRows literal nor nil; "+
					"an answer in that shape is invisible to this count, so give it the literal shape",
				name,
			)
			return true
		})
	}
	return counts
}

// answersWithRowsLiteral reports whether expr is `&contentReaderRows{...}`.
func answersWithRowsLiteral(expr ast.Expr) bool {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return false
	}
	composite, ok := unary.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	ident, ok := composite.Type.(*ast.Ident)
	return ok && ident.Name == "contentReaderRows"
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

	known := map[string]bool{
		"contentReaderFactDefaultRows":      true,
		"contentReaderReadModelDefaultRows": true,
	}
	for name := range counts {
		if !known[name] {
			t.Errorf(
				"%s looks like a new per-group default-answer helper; give it a defaultRowsGroup "+
					"and one case per branch in defaultRowsCases, then add it here",
				name,
			)
		}
	}
	for name := range known {
		if _, found := counts[name]; !found {
			t.Fatalf("%s not found in the source; was it renamed or moved?", name)
		}
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

			// Columns alone are not enough. A shared-tier branch can catch a group
			// query and answer with the same column set but different rows, which is
			// still preemption and still silently wrong for every real caller.
			wantRows := drainRows(t, owner, len(wantColumns))
			gotRows := drainRows(t, dispatched, len(gotColumns))
			if !reflect.DeepEqual(gotRows, wantRows) {
				t.Fatalf(
					"dispatcher answered %q with rows %v, group answers %v; a shared-tier branch is preempting this group query",
					testCase.query, gotRows, wantRows,
				)
			}
		})
	}
}

// drainRows reads every row out of a driver.Rows into comparable values.
//
// Both values handed to it are built fresh by the call under test, so consuming
// them is safe: nothing reads them afterwards. width comes from the caller's
// already-compared Columns() length rather than being re-derived here, so a row
// set whose arity disagrees with its own header surfaces as a scan error instead
// of being silently truncated to a shorter comparison.
func drainRows(t *testing.T, rows driver.Rows, width int) [][]driver.Value {
	t.Helper()

	var drained [][]driver.Value
	for {
		dest := make([]driver.Value, width)
		err := rows.Next(dest)
		if errors.Is(err, io.EOF) {
			return drained
		}
		if err != nil {
			t.Fatalf("read row %d: %v", len(drained), err)
		}
		drained = append(drained, dest)
	}
}
