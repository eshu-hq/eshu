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

// TestContentReaderDefaultGroupsCoverEveryAnsweringBranch requires defaultRowsCases
// to hold one case per answering branch, per group.
//
// Without this, the disjointness and shape tests both walk defaultRowsCases and
// pass no matter how short it is, so a branch added to either helper without a
// matching case is silently unguarded -- and the split of these defaults into
// per-group files is safe only while every branch is known to belong to exactly
// one group. This reads the branch count out of the source so that adding a
// branch fails here until someone adds the case, rather than being compared
// against a hand-maintained number that a rebase can quietly merge away.
func TestContentReaderDefaultGroupsCoverEveryAnsweringBranch(t *testing.T) {
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
