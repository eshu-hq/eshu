// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jsdataflow

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// lowerFirstFunction parses TypeScript src, finds the first function declaration,
// and lowers it to a CFG.
func lowerFirstFunction(t *testing.T, src string) cfg.Function {
	return lowerFirstOfKind(t, src, "function_declaration")
}

// lowerFirstOfKind parses TypeScript src, finds the first node of kind, and
// lowers it to a CFG.
func lowerFirstOfKind(t *testing.T, src, kind string) cfg.Function {
	t.Helper()
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(ts.LanguageTypescript())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	source := []byte(src)
	tree := parser.Parse(source, nil)
	defer tree.Close()

	var fnNode *tree_sitter.Node
	var walk func(*tree_sitter.Node)
	walk = func(n *tree_sitter.Node) {
		if fnNode != nil || n == nil {
			return
		}
		if n.Kind() == kind {
			captured := *n
			fnNode = &captured
			return
		}
		cursor := n.Walk()
		defer cursor.Close()
		for _, ch := range n.NamedChildren(cursor) {
			ch := ch
			walk(&ch)
		}
	}
	walk(tree.RootNode())
	if fnNode == nil {
		t.Fatalf("no %s in fixture", kind)
	}
	return LowerFunction(fnNode, source, cfg.DefaultLimits())
}

func defUseLines(fn cfg.Function) []string {
	out := make([]string, 0, len(fn.DefUses))
	for _, du := range fn.DefUses {
		out = append(out, fmt.Sprintf("%s:%d->%d", du.Binding, du.DefLine, du.UseLine))
	}
	sort.Strings(out)
	return out
}

// TestLowerArrowSingleParam proves an unparenthesized single-parameter arrow
// (req => ...) records its parameter as an entry definition.
func TestLowerArrowSingleParam(t *testing.T) {
	t.Parallel()

	src := "const h = req => {\n" +
		"\tlet x = req;\n" +
		"\tuse(x);\n" +
		"};"
	fn := lowerFirstOfKind(t, src, "arrow_function")
	got := defUseLines(fn)
	if !contains(got, "req:1->2") {
		t.Fatalf("arrow param req not flowing (let x = req); got %v", got)
	}
}

// TestLowerForInitAssignment proves a C-style for initializer that is an
// assignment (not a declaration) records its def, so it reaches the body use.
func TestLowerForInitAssignment(t *testing.T) {
	t.Parallel()

	src := "function f(n, start) {\n" +
		"\tfor (i = start; i < n; ) {\n" +
		"\t\tuse(i);\n" +
		"\t}\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "i:2->3") {
		t.Fatalf("for-init assignment def of i dropped; got %v", got)
	}
}

// TestLowerForOfDeclarationTarget proves a for-of loop target defines its
// variable, which reaches a body use.
func TestLowerForOfDeclarationTarget(t *testing.T) {
	t.Parallel()

	src := "function f(items) {\n" +
		"\tfor (const item of items) {\n" +
		"\t\tuse(item);\n" +
		"\t}\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "item:2->3") {
		t.Fatalf("for-of target item not defined; got %v", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// TestLowerIfMergeReachingDefs proves value flow through a parameter, a
// declaration, an if-branch reassignment, and a merge matches reaching-definition
// truth on a real TypeScript function.
func TestLowerIfMergeReachingDefs(t *testing.T) {
	t.Parallel()

	src := `function handler(req, db) {
	let user = req;
	if (user !== "") {
		user = sanitize(user);
	}
	let q = user;
	return q;
}`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	expect := []string{
		"req:1->2",  // let user = req reads the parameter
		"user:2->3", // if condition reads the line-2 def
		"user:2->4", // sanitize(user) on the true path reads the line-2 def
		"user:2->6", // let q = user via the false path
		"user:4->6", // let q = user via the true-path reassignment
		"q:6->7",    // return q reads the line-6 def
	}
	sort.Strings(expect)
	if !reflect.DeepEqual(got, expect) {
		t.Fatalf("def->use lines =\n  %v\nwant\n  %v", got, expect)
	}
}

// TestLowerIfElseBothBranchesReach proves a definition in the else branch (not
// just the then branch) reaches a use after the if, so the else_clause wrapper is
// descended into rather than flattened.
func TestLowerIfElseBothBranchesReach(t *testing.T) {
	t.Parallel()

	src := `function pick(flag, a, b) {
	let v = a;
	if (flag) {
		v = a;
	} else {
		v = b;
	}
	return v;
}`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	// return v (line 8) must see the then-branch def (line 4) and the
	// else-branch def (line 6); the line-2 init is killed on both paths.
	for _, want := range []string{"v:4->8", "v:6->8"} {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q (else branch lost?) in\n  %v", want, got)
		}
	}
	if contains(got, "v:2->8") {
		t.Fatalf("line-2 init should be killed on both branches, but reached the return: %v", got)
	}
}

// TestLowerForIncrementDefinesTarget proves a C-style for increment that assigns
// a variable (total = total + i) records that variable as defined, so it reaches
// a body use via the back-edge. Without lowering the increment as an assignment,
// total would never be defined and no such edge would exist.
func TestLowerForIncrementDefinesTarget(t *testing.T) {
	t.Parallel()

	src := `function count(n, total) {
	for (let i = 0; i < n; total = total + i) {
		use(total);
	}
}`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	// use(total) on line 3 must be reached by the increment's def on line 2
	// (via the back-edge). This edge only exists if the increment is lowered as
	// an assignment that defines total.
	if !contains(got, "total:2->3") {
		t.Fatalf("missing total:2->3 (for-increment def dropped) in\n  %v", got)
	}
}

// TestLowerForLoopBackEdge proves a for loop produces a back-edge so an in-loop
// definition reaches a use at the loop head alongside the pre-loop definition.
func TestLowerForLoopBackEdge(t *testing.T) {
	t.Parallel()

	src := `function accumulate(items) {
	let total = 0;
	for (let i = 0; i < items.length; i = i + 1) {
		total = total + items[i];
	}
	return total;
}`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	for _, want := range []string{"total:2->4", "total:4->4", "total:2->6", "total:4->6"} {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
}

// TestLowerReturnTerminatesBranch proves a definition in a branch that returns
// does not reach code after the if (the returned branch terminates flow).
func TestLowerReturnTerminatesBranch(t *testing.T) {
	t.Parallel()

	src := "function handler(cond) {\n" +
		"\tlet x = input;\n" +
		"\tif (cond) {\n" +
		"\t\tx = tainted;\n" +
		"\t\treturn;\n" +
		"\t}\n" +
		"\tsink(x);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "x:2->7") {
		t.Fatalf("sink(x) should see the pre-if let x = input (line 2); got %v", got)
	}
	if contains(got, "x:4->7") {
		t.Fatalf("x = tainted in the returned branch (line 4) leaked to sink(x) line 7: %v", got)
	}
}
