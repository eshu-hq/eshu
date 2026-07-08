// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonPrimaryIndexes holds the results of the full-tree index builders that
// only read root/source and do not consume each other's output mid-walk:
// dataclass-decorated class names, script `if __name__ == "__main__":` guard
// call roots, and `__all__` export names. They are collected in a single
// walkNamed pass (see buildPythonPrimaryIndexes) instead of one pass per
// builder, since each case only inspects the current node's own kind and
// never depends on a sibling case having already run.
type pythonPrimaryIndexes struct {
	dataclassClasses map[string]bool
	scriptMainRoots  map[string]bool
	moduleAllNames   map[string]struct{}
}

// buildPythonPrimaryIndexes computes pythonPrimaryIndexes in one walkNamed
// pass over root. It preserves the exact per-builder logic of the prior
// separate passes (pythonDataclassClassNames, pythonScriptMainGuardRoots, and
// the module-`__all__` scan folded into pythonPublicAPIRootKinds): each case
// below is evaluated only for its own node.Kind() and appends independently,
// so per-node visitation order and per-map contents are unchanged from
// running three separate walks.
func buildPythonPrimaryIndexes(root *tree_sitter.Node, source []byte) pythonPrimaryIndexes {
	dataclassClasses := make(map[string]bool)
	scriptMainRoots := make(map[string]bool)
	moduleAllNames := make(map[string]struct{})

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_definition":
			if len(pythonClassDeadCodeRootKinds(pythonDecorators(node, source))) == 0 {
				return
			}
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if name != "" {
				dataclassClasses[name] = true
			}
		case "if_statement":
			if !pythonIsScriptMainGuard(node, source) {
				return
			}
			pythonCollectScriptMainGuardCalls(node.ChildByFieldName("consequence"), source, scriptMainRoots)
		case "assignment":
			left := node.ChildByFieldName("left")
			if strings.TrimSpace(nodeText(left, source)) != "__all__" {
				return
			}
			for _, name := range pythonStringSequenceLiterals(node.ChildByFieldName("right"), source) {
				moduleAllNames[name] = struct{}{}
			}
		}
	})

	return pythonPrimaryIndexes{
		dataclassClasses: dataclassClasses,
		scriptMainRoots:  scriptMainRoots,
		moduleAllNames:   moduleAllNames,
	}
}
