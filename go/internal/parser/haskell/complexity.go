package haskell

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// haskellBooleanOperators are the short-circuit boolean operators that each add
// one decision point. Haskell exposes them as the named `operator` token of an
// `infix` application.
var haskellBooleanOperators = map[string]struct{}{
	"&&": {},
	"||": {},
}

// haskellEquationDecisions returns the McCabe decision points contributed by a
// single function equation (one `function` node). The base of 1 and the extra
// dispatch points for additional equations of the same function are added by the
// caller, which sees every equation; this counts only the branches inside one
// equation: guards, case alternatives, if-then-else, and boolean operators.
//
// A guard whose condition is `otherwise` or the literal `True`, and a case
// alternative that is a bare `_` wildcard, are the implicit else and are not
// counted. The walk stops at a nested `lambda` so an inner function's branches
// stay with their own scope.
func haskellEquationDecisions(node *tree_sitter.Node, source []byte) int {
	if node == nil {
		return 0
	}
	count := 0
	switch node.Kind() {
	case "lambda":
		// A lambda is its own scope; skip its body but still allow the caller to
		// have counted the equation it sits in.
		return 0
	case "guards":
		if !haskellGuardIsCatchAll(node, source) {
			count++
		}
	case "alternative":
		if !haskellAlternativeIsWildcard(node, source) {
			count++
		}
	case "conditional":
		count++
	case "infix":
		if haskellInfixIsBoolean(node, source) {
			count++
		}
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		count += haskellEquationDecisions(&child, source)
	}
	return count
}

// haskellGuardIsCatchAll reports whether a guard set is the implicit else: a
// single `otherwise` variable or the literal `True`.
func haskellGuardIsCatchAll(guards *tree_sitter.Node, source []byte) bool {
	children := haskellNamedChildren(guards)
	if len(children) != 1 {
		return false
	}
	text := strings.TrimSpace(shared.NodeText(&children[0], source))
	return text == "otherwise" || text == "True"
}

// haskellAlternativeIsWildcard reports whether a case alternative matches a bare
// `_` wildcard with no guard, which is the catch-all arm.
func haskellAlternativeIsWildcard(alternative *tree_sitter.Node, source []byte) bool {
	for _, child := range haskellNamedChildren(alternative) {
		child := child
		switch child.Kind() {
		case "wildcard":
			return true
		case "guards":
			// A guarded alternative tests conditions, so it is not a catch-all.
			return false
		}
	}
	return false
}

// haskellInfixIsBoolean reports whether an infix application's operator is a
// short-circuit boolean operator.
func haskellInfixIsBoolean(node *tree_sitter.Node, source []byte) bool {
	for _, child := range haskellNamedChildren(node) {
		child := child
		if child.Kind() != "operator" {
			continue
		}
		if _, ok := haskellBooleanOperators[strings.TrimSpace(shared.NodeText(&child, source))]; ok {
			return true
		}
	}
	return false
}
