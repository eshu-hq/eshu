// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func javaScriptParameterCount(parametersNode *tree_sitter.Node, _ []byte) int {
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
