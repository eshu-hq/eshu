package elixir

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// elixirControlMacros are the call heads that introduce a single decision point
// each: a conditional or a comprehension/loop. Elixir has no dedicated control
// grammar; if/unless/for/while are macro calls whose head is an identifier, so
// complexity counts the call by its head text rather than by node kind.
var elixirControlMacros = map[string]struct{}{
	"if":     {},
	"unless": {},
	"for":    {},
	"while":  {},
}

// elixirArmMacros are the call heads whose body is a list of `->` clauses
// (stab_clause arms). Each real arm is a decision point; the catch-all arm
// (`_ ->` or `true ->`) is the implicit else and is not counted.
var elixirArmMacros = map[string]struct{}{
	"case": {},
	"cond": {},
	"with": {},
	"try":  {},
}

// elixirBooleanOperators are the short-circuit boolean operator tokens that add
// one decision point when they head a binary_operator node.
var elixirBooleanOperators = map[string]struct{}{
	"&&":  {},
	"||":  {},
	"and": {},
	"or":  {},
}

// elixirCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// definition rooted at defNode (a def/defp/defmacro call). Elixir control flow
// is expressed as macro calls, so the count cannot come from the shared
// node-kind table; this dedicated pass reads each call's head identifier.
//
// Complexity is 1 plus one for each if/unless/for/while macro, one for each real
// case/cond/with/try arm (the `_`/`true` catch-all arm is excluded), one for
// each `when` guard, and one for each short-circuit boolean operator. The walk
// stops at nested `fn` literals and nested definition calls so an inner clause
// does not inflate the enclosing function.
func elixirCyclomaticComplexity(defNode *tree_sitter.Node, source []byte) int {
	if defNode == nil {
		return 0
	}
	complexity := 1
	body := elixirDefinitionBody(defNode)
	if body == nil {
		return complexity
	}
	complexity += elixirCountDecisions(body, source)
	return complexity
}

// elixirDefinitionBody returns the do_block of a definition call, where the
// function body lives. A guard-only or header-only definition has no do_block.
func elixirDefinitionBody(defNode *tree_sitter.Node) *tree_sitter.Node {
	for _, child := range elixirNamedChildren(defNode) {
		child := child
		if child.Kind() == "do_block" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// elixirCountDecisions sums the decision points in a subtree, stopping at nested
// function definitions and `fn` literals so their branches stay with their own
// scope.
func elixirCountDecisions(node *tree_sitter.Node, source []byte) int {
	if node == nil {
		return 0
	}
	count := 0
	switch node.Kind() {
	case "anonymous_function":
		return 0
	case "call":
		head := elixirCallHead(node, source)
		if elixirNestedScopeKeyword(head) {
			return 0
		}
		if _, ok := elixirControlMacros[head]; ok {
			count++
		}
		if _, ok := elixirArmMacros[head]; ok {
			count += elixirRealArmCount(node, source)
		}
	case "binary_operator":
		if elixirIsBooleanOperator(node, source) {
			count++
		}
	case "stab_clause":
		// A guard inside a stab arm (`pattern when cond ->`) adds a decision.
		// case/cond/with arms are counted by elixirRealArmCount on their macro,
		// so do not double count the arm itself here.
		count += elixirStabGuardCount(node, source)
	}
	for _, child := range elixirNamedChildren(node) {
		child := child
		count += elixirCountDecisions(&child, source)
	}
	return count
}

// elixirRealArmCount returns how many `->` arms of a case/cond/with/try macro
// are real decisions. The bare catch-all arm (`_ ->` or `true ->`) is the
// implicit else and is excluded.
func elixirRealArmCount(callNode *tree_sitter.Node, source []byte) int {
	arms := 0
	for _, block := range elixirNamedChildren(callNode) {
		block := block
		if block.Kind() != "do_block" && block.Kind() != "else_block" {
			continue
		}
		for _, clause := range elixirNamedChildren(&block) {
			clause := clause
			if clause.Kind() != "stab_clause" {
				continue
			}
			if elixirIsCatchAllArm(&clause, source) {
				continue
			}
			arms++
		}
	}
	return arms
}

// elixirIsCatchAllArm reports whether a stab arm is the implicit else: a bare
// `_` wildcard pattern or a literal `true` guard, neither of which tests a
// condition under McCabe.
func elixirIsCatchAllArm(clause *tree_sitter.Node, source []byte) bool {
	for _, child := range elixirNamedChildren(clause) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		args := elixirNamedChildren(&child)
		if len(args) != 1 {
			return false
		}
		switch args[0].Kind() {
		case "identifier":
			return strings.TrimSpace(shared.NodeText(&args[0], source)) == "_"
		case "boolean":
			return true
		}
	}
	return false
}

// elixirNestedScopeKeyword reports whether a call head opens a nested function
// or module scope whose own branches must not inflate the enclosing function.
func elixirNestedScopeKeyword(head string) bool {
	switch head {
	case "defmodule", "defprotocol", "defimpl":
		return true
	}
	return elixirFunctionKeyword(head)
}

// elixirStabGuardCount returns one when a stab arm carries a `when` guard, which
// is itself a condition test, and zero otherwise.
func elixirStabGuardCount(clause *tree_sitter.Node, source []byte) int {
	for _, child := range elixirNamedChildren(clause) {
		child := child
		if child.Kind() == "binary_operator" && elixirOperatorText(&child, source) == "when" {
			return 1
		}
	}
	return 0
}

// elixirIsBooleanOperator reports whether a binary_operator node's operator is a
// short-circuit boolean operator.
func elixirIsBooleanOperator(node *tree_sitter.Node, source []byte) bool {
	_, ok := elixirBooleanOperators[elixirOperatorText(node, source)]
	return ok
}

// elixirOperatorText returns the operator token text of a binary_operator node.
// The operator is the lone anonymous child between the two named operands.
func elixirOperatorText(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.Children(cursor) {
		child := child
		if !child.IsNamed() {
			return strings.TrimSpace(child.Utf8Text(source))
		}
	}
	return ""
}
