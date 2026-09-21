// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImplementedInterfaces returns the simple names of the interfaces a
// TypeScript class declares with an `implements` clause (issue #2229). Generic
// type arguments are ignored and qualified names are reduced to their last
// segment so a downstream name-resolution index can match the interface entity.
// JavaScript has no interfaces, so callers only invoke this for TypeScript.
func ImplementedInterfaces(node *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(raw string) {
		name := lastTypeSegment(strings.TrimSpace(raw))
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		if children[i].Kind() != "class_heritage" {
			continue
		}
		heritageCursor := children[i].Walk()
		heritageChildren := children[i].NamedChildren(heritageCursor)
		heritageCursor.Close()
		for j := range heritageChildren {
			if heritageChildren[j].Kind() == "implements_clause" {
				collectTypeNames(&heritageChildren[j], source, add)
			}
		}
	}

	if len(names) == 0 {
		return nil
	}
	return names
}

func collectTypeNames(n *tree_sitter.Node, source []byte, add func(string)) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "type_arguments":
		// Generic arguments are not implemented interfaces.
		return
	case "type_identifier", "nested_type_identifier":
		add(shared.NodeText(n, source))
		return
	}
	cursor := n.Walk()
	children := n.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		collectTypeNames(&children[i], source, add)
	}
}

func lastTypeSegment(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
