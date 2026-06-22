package shared

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// BranchNodeSet declares, per language, which tree-sitter node kinds and
// operator tokens count as McCabe decision points. It is plain data so adding
// cyclomatic complexity for a new language is a table entry, not new traversal
// code.
//
// The zero value counts nothing and yields a complexity of 1 for any function,
// which keeps an unconfigured language safe rather than wrong.
type BranchNodeSet struct {
	// BranchKinds are node kinds that each add one decision point: conditionals,
	// loops, switch/match arms, exception handlers, and conditional expressions.
	BranchKinds map[string]struct{}

	// NestedDefinitionKinds are node kinds whose subtrees belong to a nested
	// function, lambda, or type and must not be counted toward the enclosing
	// function's complexity.
	NestedDefinitionKinds map[string]struct{}

	// BinaryExpressionKinds are node kinds (for example binary_expression or
	// infix_expression) whose operator token is inspected for short-circuit
	// boolean operators. Leave empty to skip boolean-operator counting.
	BinaryExpressionKinds map[string]struct{}

	// BooleanOperators are operator token texts that add one decision point when
	// they appear inside a BinaryExpressionKinds node, typically "&&" and "||".
	BooleanOperators map[string]struct{}
}

// NewBranchNodeSet builds a BranchNodeSet from string slices. It is the
// constructor language tables use so call sites read as data.
func NewBranchNodeSet(
	branchKinds []string,
	nestedDefinitionKinds []string,
	binaryExpressionKinds []string,
	booleanOperators []string,
) BranchNodeSet {
	return BranchNodeSet{
		BranchKinds:           stringSet(branchKinds),
		NestedDefinitionKinds: stringSet(nestedDefinitionKinds),
		BinaryExpressionKinds: stringSet(binaryExpressionKinds),
		BooleanOperators:      stringSet(booleanOperators),
	}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// CyclomaticComplexity returns the McCabe cyclomatic complexity for the
// function subtree rooted at node. Complexity is 1 plus one for every decision
// point: each branch kind, each switch/match arm, each exception handler, each
// conditional expression, and each short-circuit boolean operator. The walk
// stops at nested definitions so an inner closure's branches do not inflate the
// enclosing function. A nil node returns 0; an empty set returns 1, matching the
// standard convention for a straight-line function.
//
// source is the parsed file bytes; it is only read to resolve boolean operator
// tokens that grammars expose as named operator nodes (for example Scala's
// operator_identifier). When BinaryExpressionKinds is empty source is unused and
// may be nil.
//
// The walk visits every node (named and anonymous) because short-circuit
// operators such as && and || are anonymous tokens inside a binary expression
// in most tree-sitter grammars.
func CyclomaticComplexity(node *tree_sitter.Node, source []byte, set BranchNodeSet) int {
	if node == nil {
		return 0
	}

	complexity := 1
	var walk func(current *tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current != node {
			if _, nested := set.NestedDefinitionKinds[current.Kind()]; nested {
				return
			}
		}
		if _, branch := set.BranchKinds[current.Kind()]; branch {
			complexity++
		}
		if _, binary := set.BinaryExpressionKinds[current.Kind()]; binary {
			complexity += booleanOperatorCount(current, source, set.BooleanOperators)
		}

		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.Children(cursor) {
			child := child
			walk(&child)
		}
	}

	walk(node)
	return complexity
}

// booleanOperatorCount returns how many direct operator children of a binary or
// infix expression node are short-circuit boolean operators. It inspects direct
// children only so a nested binary expression is counted when its own node is
// visited, never twice. Anonymous operator tokens (&&, ||) are matched by node
// kind; grammars that wrap the operator in a named node match by token text.
func booleanOperatorCount(node *tree_sitter.Node, source []byte, operators map[string]struct{}) int {
	if node == nil || len(operators) == 0 {
		return 0
	}
	count := 0
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.Children(cursor) {
		child := child
		if _, ok := operators[child.Kind()]; ok {
			count++
			continue
		}
		if child.IsNamed() {
			if _, ok := operators[child.Utf8Text(source)]; ok {
				count++
			}
		}
	}
	return count
}
