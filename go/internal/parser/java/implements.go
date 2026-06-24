// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaImplementedInterfaces returns the simple names of the interfaces a class,
// record, or enum declares with an `implements` clause (issue #2229). Generic
// type arguments are ignored so `implements List<String>` yields `List`, and
// qualified names are reduced to their last segment so a downstream
// name-resolution index can match the interface entity.
func javaImplementedInterfaces(node *tree_sitter.Node, source []byte) []string {
	interfacesNode := node.ChildByFieldName("interfaces")
	if interfacesNode == nil {
		return nil
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(raw string) {
		name := javaLastTypeSegment(strings.TrimSpace(raw))
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	var collect func(n *tree_sitter.Node)
	collect = func(n *tree_sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "type_arguments":
			// Generic arguments are not implemented interfaces.
			return
		case "type_identifier", "scoped_type_identifier":
			add(nodeText(n, source))
			return
		}
		walkDirectNamed(n, collect)
	}
	collect(interfacesNode)

	if len(names) == 0 {
		return nil
	}
	return names
}

func javaLastTypeSegment(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
