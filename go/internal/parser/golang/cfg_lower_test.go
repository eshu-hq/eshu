// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// lowerFirstFunction parses src, finds the first function/method declaration,
// lowers it to a CFG, and returns the resolved Function.
func lowerFirstFunction(t *testing.T, src string) cfg.Function {
	t.Helper()
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(golang.Language())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	source := []byte(src)
	tree := parser.Parse(source, nil)
	defer tree.Close()

	var fnNode *tree_sitter.Node
	root := tree.RootNode()
	walk(root, func(n *tree_sitter.Node) bool {
		if fnNode != nil {
			return false
		}
		if n.Kind() == "function_declaration" || n.Kind() == "method_declaration" {
			captured := *n
			fnNode = &captured
			return false
		}
		return true
	})
	if fnNode == nil {
		t.Fatalf("no function declaration found in fixture")
	}
	return goLowerFunction(fnNode, source, cfg.DefaultLimits())
}

// walk visits every named node depth-first until visit returns false to stop
// descending. It is a tiny local helper for the test only.
func walk(node *tree_sitter.Node, visit func(*tree_sitter.Node) bool) {
	if node == nil || !visit(node) {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			walk(child, visit)
		}
		if !cursor.GotoNextSibling() {
			return
		}
	}
}

// defUseLines renders def->use edges as binding:defLine->useLine tuples so a
// fixture author can read the expectations directly off the source.
func defUseLines(fn cfg.Function) []string {
	out := make([]string, 0, len(fn.DefUses))
	for _, du := range fn.DefUses {
		out = append(out, fmt.Sprintf("%s:%d->%d", du.Binding, du.DefLine, du.UseLine))
	}
	sort.Strings(out)
	return out
}

// TestLowerIfMergeReachingDefs proves value flow through a parameter, an
// assignment, an if-branch reassignment, and a merge matches reaching-definition
// truth on a real Go function.
func TestLowerIfMergeReachingDefs(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(req string) {
	user := req
	if user != "" {
		user = sanitize(user)
	}
	query := user
	_ = query
}

func sanitize(s string) string { return s }
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	expect := []string{
		"req:3->4",   // user := req reads the param defined on the signature line
		"user:4->5",  // if user != "" reads the line-4 definition
		"user:4->6",  // sanitize(user) on the true path reads the line-4 definition
		"user:4->8",  // query := user via the false path
		"user:6->8",  // query := user via the true-path reassignment
		"query:8->9", // _ = query reads the line-8 definition
	}
	sort.Strings(expect)
	if !reflect.DeepEqual(got, expect) {
		t.Fatalf("def->use lines =\n  %v\nwant\n  %v", got, expect)
	}
}

// TestLowerForLoopBackEdge proves a for loop produces a back-edge so an in-loop
// definition reaches a use at the loop head alongside the pre-loop definition.
func TestLowerForLoopBackEdge(t *testing.T) {
	t.Parallel()

	src := `package main

func accumulate(items []int) int {
	total := 0
	for i := 0; i < len(items); i++ {
		total = total + items[i]
	}
	return total
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	// total is defined at line 4 (init) and line 6 (in-loop). The in-loop use of
	// total at line 6 must see both; the return at line 8 sees both as well.
	mustContain := []string{
		"total:4->6",
		"total:6->6",
		"total:4->8",
		"total:6->8",
	}
	for _, want := range mustContain {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
}

// TestLowerCompoundAssignment proves a compound assignment (+=) reads its target
// as well as writing it, so the prior definition reaches the compound statement.
func TestLowerCompoundAssignment(t *testing.T) {
	t.Parallel()

	src := `package main

func acc(seed int) int {
	total := seed
	total += seed
	return total
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	mustContain := []string{
		"seed:3->4",  // total := seed
		"total:4->5", // total += seed reads the line-4 def (compound operator)
		"seed:3->5",  // total += seed also reads seed
		"total:5->6", // return total reads the compound def
	}
	for _, want := range mustContain {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
}

// TestLowerForRange proves a for-range loop defines its loop variables at the
// header (reading the ranged expression) and that those definitions reach a use
// in the body.
func TestLowerForRange(t *testing.T) {
	t.Parallel()

	src := `package main

func total(items []int) int {
	sum := 0
	for _, item := range items {
		sum = sum + item
	}
	return sum
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	mustContain := []string{
		"items:3->5", // range items reads the parameter
		"item:5->6",  // the range-bound item reaches sum = sum + item
		"sum:4->6",   // sum init reaches the in-loop use
		"sum:6->6",   // in-loop sum reaches itself via the back-edge
	}
	for _, want := range mustContain {
		if !contains(got, want) {
			t.Fatalf("missing def->use %q in\n  %v", want, got)
		}
	}
}

// TestLowerVarDeclaration proves a var declaration with an initializer defines
// its name and reads the initializer, without treating the initializer's
// identifier as a definition.
func TestLowerVarDeclaration(t *testing.T) {
	t.Parallel()

	src := `package main

func pick(in string) string {
	var out = in
	return out
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	want := []string{
		"in:3->4",  // var out = in reads the parameter
		"out:4->5", // return out reads the var definition
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("def->use lines =\n  %v\nwant\n  %v", got, want)
	}
}

// TestLowerSelectorAccessPathsAreFieldSensitive proves selector assignments
// define the selected field path and selector reads use that same field path
// rather than collapsing all fields into the base binding.
func TestLowerSelectorAccessPathsAreFieldSensitive(t *testing.T) {
	t.Parallel()

	src := `package main

type payload struct{ SQL, Display string }

func handler(in string) {
	var data payload
	data.SQL = in
	data.Display = "safe"
	sink(data.SQL)
	sink(data.Display)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "data.SQL:7->9") {
		t.Fatalf("missing data.SQL field def reaching data.SQL sink in\n  %v", got)
	}
	if contains(got, "data.SQL:7->10") {
		t.Fatalf("data.SQL def reached sibling data.Display sink in\n  %v", got)
	}
}

// TestLowerPointerAliasSelectorAccessPath proves a simple pointer alias to a
// local struct normalizes selector writes back to the original field path.
func TestLowerPointerAliasSelectorAccessPath(t *testing.T) {
	t.Parallel()

	src := `package main

type payload struct{ SQL string }

func handler(in string) {
	var data payload
	alias := &data
	alias.SQL = in
	sink(data.SQL)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "data.SQL:8->9") {
		t.Fatalf("missing aliased data.SQL field def reaching data.SQL sink in\n  %v", got)
	}
}

// TestLowerPointerAliasChainSelectorAccessPath proves alias chains over an
// existing pointer alias still normalize selector writes to the original field.
func TestLowerPointerAliasChainSelectorAccessPath(t *testing.T) {
	t.Parallel()

	src := `package main

type payload struct{ SQL string }

func handler(in string) {
	var data payload
	alias := &data
	next := alias
	next.SQL = in
	sink(data.SQL)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "data.SQL:9->10") {
		t.Fatalf("missing chained alias data.SQL field def reaching data.SQL sink in\n  %v", got)
	}
}

// TestLowerIfInitializerPointerAliasDoesNotLeak proves an alias declared in an
// if initializer is scoped to that if statement and does not replace an outer
// alias after the merge.
func TestLowerIfInitializerPointerAliasDoesNotLeak(t *testing.T) {
	t.Parallel()

	src := `package main

type payload struct{ SQL string }

func handler(in string, cond bool) {
	var outer payload
	var data payload
	alias := &outer
	if alias := &data; cond {
		alias.SQL = "branch"
	}
	alias.SQL = in
	sink(data.SQL)
	sink(outer.SQL)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if contains(got, "data.SQL:12->13") {
		t.Fatalf("if initializer alias leaked to post-if assignment in\n  %v", got)
	}
	if !contains(got, "outer.SQL:12->14") {
		t.Fatalf("outer alias was not restored for post-if assignment in\n  %v", got)
	}
}

// TestLowerStructValueCopyDoesNotAliasFieldMutation proves a plain value copy is
// not treated as a pointer alias for later selector writes.
func TestLowerStructValueCopyDoesNotAliasFieldMutation(t *testing.T) {
	t.Parallel()

	src := `package main

type payload struct{ SQL string }

func handler(in string) {
	var data payload
	copy := data
	copy.SQL = in
	sink(data.SQL)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if contains(got, "copy.SQL:8->9") || contains(got, "data.SQL:8->9") {
		t.Fatalf("plain struct value copy must not alias later field mutation in\n  %v", got)
	}
}

// TestLowerContainerElementAccessUsesWholeContainerApproximation proves indexed
// writes and reads use an explicit container-element approximation.
func TestLowerContainerElementAccessUsesWholeContainerApproximation(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(in string) {
	values := map[string]string{}
	values["query"] = in
	sink(values["query"])
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "values[*]:5->6") {
		t.Fatalf("missing values[*] container def reaching indexed sink in\n  %v", got)
	}
}

// TestLowerClosureCaptureUsesOuterDefinition proves function-literal bodies
// contribute captured-variable uses to the enclosing dataflow graph.
func TestLowerClosureCaptureUsesOuterDefinition(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(in string) {
	value := in
	func() {
		sink(value)
	}()
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "value:4->5") {
		t.Fatalf("missing captured value def reaching closure body sink in\n  %v", got)
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

// TestLowerReturnTerminatesBranch proves a definition in a branch that returns
// does not reach code after the if (the returned branch terminates flow, so the
// reaching-def graph does not gain a false edge).
func TestLowerReturnTerminatesBranch(t *testing.T) {
	t.Parallel()

	src := `package main

func handler(cond bool) {
	x := input
	if cond {
		x = tainted
		return
	}
	sink(x)
}
`
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)

	if !contains(got, "x:4->9") {
		t.Fatalf("sink(x) should see the pre-if x := input (line 4); got %v", got)
	}
	if contains(got, "x:6->9") {
		t.Fatalf("x = tainted in the returned branch (line 6) leaked to sink(x) line 9: %v", got)
	}
}

// TestLowerLabeledStmtAfterReturnNotDropped proves a labeled statement after a
// return (a potential goto target) is not dropped from the CFG.
func TestLowerLabeledStmtAfterReturnNotDropped(t *testing.T) {
	t.Parallel()

	src := `package main

func f(x int) {
	return
L:
	sink(x)
	goto L
}
`
	fn := lowerFirstFunction(t, src)
	hasUseOfX := false
	for _, b := range fn.Blocks {
		for _, s := range b.Stmts {
			for _, u := range s.Uses {
				if u == "x" {
					hasUseOfX = true
				}
			}
		}
	}
	if !hasUseOfX {
		t.Fatalf("labeled sink(x) after return was dropped from the CFG; blocks=%+v", fn.Blocks)
	}
}
