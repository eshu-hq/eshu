// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// phpParentLookup amortizes ancestor traversal cost from O(depth) per
// tree-sitter Parent() call to O(1) per lookup after a one-time O(n) build.
//
// PHP call and variable extraction asks for enclosing scope, type context, and
// function context for many nodes. Calling Node.Parent() for each step crosses
// cgo into tree-sitter, and profiles on legacy PHP repositories showed those
// parent crossings dominating parse CPU. Build one lookup per Parse tree and
// share it read-only through the parser state instead.
type phpParentLookup struct {
	parents map[uintptr]*tree_sitter.Node
}

// buildPHPParentLookup records every child->parent edge in one tree-sitter pass
// so scope and context helpers avoid repeated cgo Parent() crossings. It
// indexes named and unnamed children because ancestor chains can pass through
// unnamed grammar wrappers.
func buildPHPParentLookup(root *tree_sitter.Node) *phpParentLookup {
	recordFullWalkForTest()
	lookup := &phpParentLookup{parents: make(map[uintptr]*tree_sitter.Node)}
	if root == nil {
		return lookup
	}
	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := node.ChildCount()
		for i := range count {
			child := node.Child(i)
			if child == nil {
				continue
			}
			lookup.parents[child.Id()] = node
			stack = append(stack, child)
		}
	}
	return lookup
}

func (l *phpParentLookup) parent(node *tree_sitter.Node) *tree_sitter.Node {
	if l == nil || node == nil {
		return nil
	}
	return l.parents[node.Id()]
}
