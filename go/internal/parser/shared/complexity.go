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

	// DefaultCaseKinds are BranchKinds whose node also covers a switch/match
	// catch-all arm: a `default` label (Java switch_label, C# switch_section,
	// C/C++ case_statement) or a bare wildcard `_` arm (Rust match_arm, Scala and
	// Python case_clause). Under McCabe the catch-all is the implicit else /
	// fall-through path, not an additional decision, so a node of one of these
	// kinds is counted only when it is a real case arm (see isCatchAllArm).
	// Grammars with a distinct catch-all node (Go's default_case) do not need an
	// entry here because that kind is simply absent from BranchKinds.
	DefaultCaseKinds map[string]struct{}
}

// NewBranchNodeSet builds a BranchNodeSet from string slices. It is the
// constructor language tables use so call sites read as data. defaultCaseKinds
// names the BranchKinds that double as switch `default` labels and must be
// counted only for real case arms; pass nil when the grammar has a distinct
// default node.
func NewBranchNodeSet(
	branchKinds []string,
	nestedDefinitionKinds []string,
	binaryExpressionKinds []string,
	booleanOperators []string,
	defaultCaseKinds []string,
) BranchNodeSet {
	return BranchNodeSet{
		BranchKinds:           stringSet(branchKinds),
		NestedDefinitionKinds: stringSet(nestedDefinitionKinds),
		BinaryExpressionKinds: stringSet(binaryExpressionKinds),
		BooleanOperators:      stringSet(booleanOperators),
		DefaultCaseKinds:      stringSet(defaultCaseKinds),
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
		// Only named nodes are control-flow constructs. Some grammars
		// (tree-sitter-ruby) name the statement node and its leading keyword
		// token the same kind, e.g. both the `if` statement and the anonymous
		// `if` keyword report Kind() == "if"; counting the keyword token would
		// double every such branch. Anonymous tokens never carry a decision.
		if current.IsNamed() {
			if _, branch := set.BranchKinds[current.Kind()]; branch {
				if _, defaultable := set.DefaultCaseKinds[current.Kind()]; !defaultable || !isCatchAllArm(current) {
					complexity++
				}
			}
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

// isCatchAllArm reports whether a switch/match arm is the catch-all (implicit
// else) rather than a real decision. Under McCabe the catch-all carries no
// condition test, so it adds no decision point. The recognized grammar shapes
// are:
//
//   - a direct anonymous `default` token child (Java switch_label, C#
//     switch_section, C/C++ case_statement reuse one node kind for `case` and
//     `default`);
//   - a direct anonymous `else` token child (Kotlin when_entry `else ->` and
//     Ruby/grammars whose single arm kind covers the catch-all with an `else`
//     keyword);
//   - a direct named `default_keyword` child (Swift switch_entry reuses one
//     node kind for `case` and `default`);
//   - a direct `wildcard` node child (Scala case_clause `case _ =>`); and
//   - a match_pattern/case_pattern child that is a bare `_` wildcard (Rust
//     match_arm, Python case_clause). A guarded wildcard (`_ if cond`) parses
//     with the guard inside the pattern, giving it more than one child, so it is
//     not treated as a catch-all and stays counted because the guard is itself a
//     decision.
func isCatchAllArm(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.Children(cursor) {
		child := child
		switch {
		case !child.IsNamed() && (child.Kind() == "default" || child.Kind() == "else"):
			return true
		case child.Kind() == "wildcard" || child.Kind() == "default_keyword":
			return true
		case child.Kind() == "match_pattern" || child.Kind() == "case_pattern":
			if isBareWildcardPattern(&child) {
				return true
			}
		}
	}
	return false
}

// isBareWildcardPattern reports whether a match_pattern/case_pattern is exactly
// the wildcard `_` with no guard. A wildcard nested inside a larger pattern (for
// example `Some(_)` or `[_, _]`) is not a catch-all because the surrounding
// pattern still tests structure, and a guarded wildcard (`_ if cond`) carries
// extra children, so only a single direct `_` token qualifies.
func isBareWildcardPattern(pattern *tree_sitter.Node) bool {
	if pattern == nil {
		return false
	}
	cursor := pattern.Walk()
	defer cursor.Close()
	children := pattern.Children(cursor)
	if len(children) != 1 {
		return false
	}
	return children[0].Kind() == "_"
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
