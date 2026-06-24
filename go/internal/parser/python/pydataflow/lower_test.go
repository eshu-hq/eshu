// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pydataflow

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// lowerFirstFunction parses Python src, finds the first function definition, and
// lowers it to a CFG.
func lowerFirstFunction(t *testing.T, src string) cfg.Function {
	t.Helper()
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(py.Language())); err != nil {
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
		if n.Kind() == "function_definition" {
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
		t.Fatalf("no function definition in fixture")
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

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// TestLowerIfElseMergeReachingDefs proves value flow through a parameter, an
// if/else with both branches redefining a variable, and a merge matches
// reaching-definition truth on a real Python function.
func TestLowerIfElseMergeReachingDefs(t *testing.T) {
	t.Parallel()

	src := "def handler(req, db):\n" +
		"    user = req\n" +
		"    if user != \"\":\n" +
		"        user = sanitize(user)\n" +
		"    else:\n" +
		"        user = other\n" +
		"    q = user\n" +
		"    return q\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	expect := []string{
		"req:1->2",  // user = req reads the parameter
		"user:2->3", // if condition reads the line-2 def
		"user:2->4", // sanitize(user) on the then path reads the line-2 def
		"user:4->7", // q = user via the then-path reassignment
		"user:6->7", // q = user via the else-path reassignment
		"q:7->8",    // return q reads the line-7 def
	}
	sort.Strings(expect)
	if !reflect.DeepEqual(got, expect) {
		t.Fatalf("def->use lines =\n  %v\nwant\n  %v", got, expect)
	}
}

// TestLowerIfElifElseChain proves an if/elif/else chain reaches the merge from
// every branch and, crucially, does NOT leak a pre-if definition through an
// elif's false path (a false reaching definition would be a false edge).
func TestLowerIfElifElseChain(t *testing.T) {
	t.Parallel()

	src := "def classify(x):\n" +
		"    r = 0\n" +
		"    if x == 1:\n" +
		"        r = 10\n" +
		"    elif x == 2:\n" +
		"        r = 20\n" +
		"    else:\n" +
		"        r = 30\n" +
		"    return r\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	for _, want := range []string{
		"x:1->3", // if condition reads the param
		"x:1->5", // elif condition reads the param
		"r:4->9", // return reaches the if-branch def
		"r:6->9", // return reaches the elif-branch def
		"r:8->9", // return reaches the else-branch def
	} {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
	if contains(got, "r:2->9") {
		t.Fatalf("pre-if r=0 (line 2) leaked to the return through the elif fall-through: %v", got)
	}
}

// TestLowerForLoopBackEdge proves a Python for-in loop defines its target each
// iteration and produces a back-edge so an in-loop definition reaches a use at
// the loop head alongside the pre-loop definition.
func TestLowerForLoopBackEdge(t *testing.T) {
	t.Parallel()

	src := "def accumulate(items):\n" +
		"    total = 0\n" +
		"    for i in items:\n" +
		"        total = total + i\n" +
		"    return total\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	for _, want := range []string{
		"items:1->3", // for i in items reads the parameter
		"i:3->4",     // the loop target reaches the body use
		"total:2->4", // total init reaches the in-loop use
		"total:4->4", // in-loop total reaches itself via the back-edge
		"total:2->5", // return total if the loop never runs
		"total:4->5", // return total after the loop
	} {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
}

// TestLowerReturnTerminatesBranch proves a definition in a branch that returns
// does NOT reach code after the if — the returned branch terminates flow rather
// than falling through to the merge (which would be a false reaching definition).
func TestLowerReturnTerminatesBranch(t *testing.T) {
	t.Parallel()

	src := "def f(cond):\n" +
		"    x = safe\n" +
		"    if cond:\n" +
		"        x = tainted\n" +
		"        return x\n" +
		"    sink(x)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "x:2->6") {
		t.Fatalf("sink(x) should see the pre-if x=safe (line 2); got %v", got)
	}
	if contains(got, "x:4->6") {
		t.Fatalf("x=tainted in the returned branch (line 4) leaked to sink(x) line 6: %v", got)
	}
}

// TestLowerKeywordArgNameNotAUse proves a keyword argument name (sink(user=value))
// is not recorded as a variable use, only its value side is.
func TestLowerKeywordArgNameNotAUse(t *testing.T) {
	t.Parallel()

	src := "def g(user, value):\n" +
		"    sink(user=value)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "value:1->2") {
		t.Fatalf("the keyword value (value) should be a use; got %v", got)
	}
	if contains(got, "user:1->2") {
		t.Fatalf("the keyword name (user) was wrongly recorded as a variable use: %v", got)
	}
}
