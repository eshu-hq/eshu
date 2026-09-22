// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// ParameterCount returns the number of named children of a parameters node
// (a function/method's formal parameter list), or 0 for a nil node.
func ParameterCount(parametersNode *tree_sitter.Node, _ []byte) int {
	if parametersNode == nil {
		return 0
	}
	cursor := parametersNode.Walk()
	defer cursor.Close()
	count := 0
	for range parametersNode.NamedChildren(cursor) {
		count++
	}
	return count
}
