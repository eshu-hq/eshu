// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func walkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	shared.WalkNamed(node, visit)
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	return shared.NodeText(node, source)
}

func nodeLine(node *tree_sitter.Node) int {
	return shared.NodeLine(node)
}

func nodeEndLine(node *tree_sitter.Node) int {
	return shared.NodeEndLine(node)
}

// goComplexitySet declares the Go tree-sitter node kinds and boolean operator
// tokens that count as McCabe decision points. It is data so Go complexity stays
// consistent with the shared walker used by every other language.
//
// The Go grammar exposes a distinct `default_case` node for `default:`, separate
// from `expression_case`/`type_case`/`communication_case`. Under McCabe the
// default arm is the implicit else, not a decision point, so `default_case` is
// deliberately omitted from the branch kinds (and no default-case override is
// needed). A switch whose only arm is `default:` therefore stays complexity 1.
var goComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"expression_case",
		"type_case",
		"communication_case",
	},
	[]string{"function_declaration", "method_declaration", "func_literal"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	nil,
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, goComplexitySet)
}
