// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"bytes"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file proves the receiver of a #8280 PHP instance key. A variable scope
// is a function, method, or closure body, or the script; an arrow function
// shares its parent's scope. A receiver $v is proven when its scope writes it
// exactly once, by `new X(...)`, and every other occurrence is the object of a
// member call or property access.

// phpScopeVariables summarizes how one variable scope uses the candidate
// variables (those some `new` of a locked class writes).
type phpScopeVariables struct {
	// blocked is set when the scope's text holds a construct that can write
	// variables invisibly: include or require, a variable variable ($$v or
	// ${...}), extract, parse_str, eval, $GLOBALS, a goto that can skip the
	// write, or ticks that run a function after any statement. The check is textual and errs toward
	// blocking (a word in a comment or string blocks too), which only costs
	// recall.
	blocked bool
	vars    map[string]*phpVariableUse
}

// phpVariableUse counts the occurrences of one variable in a scope.
type phpVariableUse struct {
	// newWrites counts `$v = new X(...)` assignments; assignment and creation
	// are the last one's assignment_expression and object_creation_expression.
	newWrites  int
	assignment *tree_sitter.Node
	creation   *tree_sitter.Node
	// other counts every occurrence that is neither such a write nor the
	// object of a member call or property access: an argument (a possible
	// by-reference write), foreach, list(), catch, a parameter, global,
	// static, or a closure use clause.
	other int
}

// phpScopeBlockers are the lowercase texts whose presence in a scope can
// write a variable without naming it.
var phpScopeBlockers = [][]byte{
	[]byte("include"), []byte("require"), []byte("$$"), []byte("${"), []byte("extract"), []byte("parse_str"),
	[]byte("eval"), []byte("$globals"), []byte("goto"), []byte("ticks"),
}

// phpScriptRegionBlockers are the lowercase texts that, between a
// script-scope write and its call, may run code that reaches the script's
// variables: an include, an eval, output that may call __toString or an
// output buffer callback (echo, print, <?=, or inline HTML after ?>), a clone
// that calls __clone, a shell command, a global declaration, or a class
// constant or static member that may run an autoloader (::).
var phpScriptRegionBlockers = [][]byte{
	[]byte("include"), []byte("require"), []byte("eval"), []byte("echo"), []byte("print"), []byte("<?="),
	[]byte("?>"), []byte("clone"), []byte("`"), []byte("global"), []byte("::"),
}

// scopeSummaries returns, built once per file, the summary of every scope that
// holds an occurrence of a candidate variable, keyed by scope root node ID. It
// reads the variable_name nodes phase 1 already gathered and classifies only
// the occurrences of candidate names, so a file pays for no tree walk.
func (k *phpPackageKeys) scopeSummaries(state *phpParseState) map[uintptr]*phpScopeVariables {
	if k.scopes != nil {
		return k.scopes
	}
	k.scopes = map[uintptr]*phpScopeVariables{}
	candidates := k.newAssignedVariables(state)
	if len(candidates) == 0 {
		return k.scopes
	}
	for _, node := range state.gatheredPhase2Nodes {
		if node.Kind() != "variable_name" {
			continue
		}
		name := shared.NodeText(node, state.source)
		if _, ok := candidates[name]; !ok {
			continue
		}
		parent := state.parents.parent(node)
		if parent == nil {
			continue
		}
		if parent.Kind() == "by_ref" {
			parent = state.parents.parent(parent)
		}
		if parent != nil && parent.Kind() == "anonymous_function_use_clause" {
			// `use ($v)` and `use (&$v)` bind the closure's own $v and, by
			// reference, can write the outer one: an other-occurrence of both.
			if closure := state.parents.parent(parent); closure != nil {
				k.scopeFor(phpVariableScopeRoot(closure, state.parents), state.source).use(name).other++
			}
			k.scopeFor(phpVariableScopeRoot(node, state.parents), state.source).use(name).other++
			continue
		}
		k.scopeFor(phpVariableScopeRoot(node, state.parents), state.source).record(parent, node, name)
	}
	return k.scopes
}

// scopeFor returns the summary of the scope rooted at root, creating it with
// its textual blocker check.
func (k *phpPackageKeys) scopeFor(root *tree_sitter.Node, source []byte) *phpScopeVariables {
	if scope, ok := k.scopes[root.Id()]; ok {
		return scope
	}
	scope := &phpScopeVariables{vars: map[string]*phpVariableUse{}}
	if start, end := root.StartByte(), root.EndByte(); end <= uint(len(source)) && start <= end {
		text := bytes.ToLower(source[start:end])
		for _, blocker := range phpScopeBlockers {
			if bytes.Contains(text, blocker) {
				scope.blocked = true
				break
			}
		}
	}
	k.scopes[root.Id()] = scope
	return scope
}

// record classifies one occurrence of variable name by its parent.
func (s *phpScopeVariables) record(parent *tree_sitter.Node, variable *tree_sitter.Node, name string) {
	use := s.use(name)
	switch parent.Kind() {
	case "assignment_expression":
		left := parent.ChildByFieldName("left")
		right := parent.ChildByFieldName("right")
		if left != nil && left.Id() == variable.Id() && right != nil && right.Kind() == "object_creation_expression" {
			use.newWrites++
			use.assignment = parent
			use.creation = right
			return
		}
	case "member_call_expression", "member_access_expression",
		"nullsafe_member_call_expression", "nullsafe_member_access_expression":
		if object := parent.ChildByFieldName("object"); object != nil && object.Id() == variable.Id() {
			return
		}
	}
	use.other++
}

func (s *phpScopeVariables) use(name string) *phpVariableUse {
	use := s.vars[name]
	if use == nil {
		use = &phpVariableUse{}
		s.vars[name] = use
	}
	return use
}

// newAssignedVariables returns, built once per file, the names of the
// variables some assignment writes with `new` of a class a lock provides. A
// member call on any other variable cannot be keyed.
func (k *phpPackageKeys) newAssignedVariables(state *phpParseState) map[string]struct{} {
	if k.newAssigned != nil {
		return k.newAssigned
	}
	k.newAssigned = map[string]struct{}{}
	for _, node := range state.gatheredPhase2Nodes {
		if node.Kind() != "object_creation_expression" {
			continue
		}
		assignment := state.parents.parent(node)
		if assignment == nil || assignment.Kind() != "assignment_expression" {
			continue
		}
		left := assignment.ChildByFieldName("left")
		if left == nil || left.Kind() != "variable_name" {
			continue
		}
		if fqcn := k.classTarget(phpObjectCreationTypeNode(node), state.source); fqcn != "" && k.lockedPackage(fqcn) != "" {
			k.newAssigned[shared.NodeText(left, state.source)] = struct{}{}
		}
	}
	return k.newAssigned
}

// phpVariableScopeRoot returns the node whose subtree is node's variable
// scope: the nearest function, method, or closure, or the program root. An
// arrow function shares its parent's scope.
func phpVariableScopeRoot(node *tree_sitter.Node, parents *phpParentLookup) *tree_sitter.Node {
	root := node
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_definition", "method_declaration", "anonymous_function":
			return current
		}
		root = current
	}
	return root
}

// phpWriteDominates reports whether the statement holding assignment is an
// earlier sibling of the call or of one of its ancestors, so the write runs
// before the call on every path through the block they share.
func phpWriteDominates(assignment *tree_sitter.Node, call *tree_sitter.Node, parents *phpParentLookup) bool {
	statement := parents.parent(assignment)
	if statement == nil || statement.Kind() != "expression_statement" {
		return false
	}
	block := parents.parent(statement)
	if block == nil {
		return false
	}
	for current := call; current != nil; current = parents.parent(current) {
		parent := parents.parent(current)
		if parent == nil {
			return false
		}
		if parent.Id() == block.Id() {
			return statement.EndByte() <= current.StartByte()
		}
	}
	return false
}

// phpScriptRegionInert reports whether nothing between a script-scope write
// (the statement holding assignment) and call can change receiver. A script's
// variables are reachable from any function through `global` or `$GLOBALS`,
// even a function in another file, so the region may hold no function,
// static, or constructor call, no member call or property access on another
// object, no read of any other variable, and none of phpScriptRegionBlockers.
// Literals and member calls on receiver itself are allowed.
func phpScriptRegionInert(state *phpParseState, assignment *tree_sitter.Node, call *tree_sitter.Node, receiver string) bool {
	statement := state.parents.parent(assignment)
	if statement == nil {
		return false
	}
	start, end := statement.EndByte(), call.StartByte()
	if start > end || end > uint(len(state.source)) {
		return false
	}
	text := bytes.ToLower(state.source[start:end])
	for _, blocker := range phpScriptRegionBlockers {
		if bytes.Contains(text, blocker) {
			return false
		}
	}
	for _, node := range state.gatheredPhase2Nodes {
		if node.StartByte() < start || node.StartByte() >= end {
			continue
		}
		switch node.Kind() {
		case "variable_name":
			if shared.NodeText(node, state.source) != receiver {
				return false
			}
		case "function_call_expression", "scoped_call_expression", "object_creation_expression":
			return false
		case "member_call_expression", "nullsafe_member_call_expression":
			object := node.ChildByFieldName("object")
			if object == nil || object.Kind() != "variable_name" || shared.NodeText(object, state.source) != receiver {
				return false
			}
		}
	}
	return true
}
