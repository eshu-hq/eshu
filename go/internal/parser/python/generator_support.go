package python

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func pythonFunctionIsGenerator(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}

	var walk func(*tree_sitter.Node) bool
	walk = func(current *tree_sitter.Node) bool {
		if current == nil {
			return false
		}
		if current != node && isNestedDefinition(current.Kind()) {
			return false
		}
		switch current.Kind() {
		case "yield", "yield_expression":
			return true
		}

		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if walk(&child) {
				return true
			}
		}
		return false
	}

	return walk(node)
}
