// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonEnclosingClassName(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "class_definition" {
			continue
		}
		name := strings.TrimSpace(nodeText(current.ChildByFieldName("name"), source))
		if name != "" {
			return name
		}
	}
	return ""
}

// pythonClassBaseNames reads positional superclass references straight from the
// tree-sitter `superclasses` argument list. Keyword arguments (for example
// metaclass=) and comments are skipped; each remaining argument contributes its
// trailing dotted name, deduplicated and sorted for a stable payload. Reading
// the AST node instead of the class header text means multi-line base lists are
// captured the same as single-line ones.
func pythonClassBaseNames(node *tree_sitter.Node, source []byte) []string {
	superclasses := node.ChildByFieldName("superclasses")
	if superclasses == nil {
		return nil
	}
	bases := make([]string, 0)
	cursor := superclasses.Walk()
	defer cursor.Close()
	for _, child := range superclasses.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "keyword_argument", "comment":
			continue
		}
		base := pythonTrailingName(strings.TrimSpace(nodeText(&child, source)))
		if base == "" {
			continue
		}
		bases = appendUniqueString(bases, base)
	}
	slices.Sort(bases)
	return bases
}
