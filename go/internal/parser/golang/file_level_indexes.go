// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goCollectFileLevelIndexes builds the import-alias index, the same-file
// constructor-return-type index, and the local-name-binding index for one
// file in a single shared.WalkNamed pass over root, instead of the three
// independent full-tree walks that symbols.ImportAliasIndex, the former
// goConstructorReturnTypes, and the former goLocalNameBindings each used to
// perform separately for the same Parse call (see #4839). Every node kind
// dispatches to the exact single-node visitor its former standalone walk
// used, so the combined output is unchanged; only the walk itself is merged.
//
// This builder is specific to Parse's own per-file root. Package pre-scan
// passes that walk other roots (a package's other files) keep calling the
// standalone symbols.ImportAliasIndex, since those walks are not part of this
// file's redundant three-walk set.
func goCollectFileLevelIndexes(
	root *tree_sitter.Node,
	source []byte,
	lookup *symbols.ParentLookup,
) (map[string][]string, map[string]string, []symbols.LocalNameBinding) {
	importAliases := make(map[string][]string)
	constructorReturns := make(map[string]string)
	localNameBindings := make([]symbols.LocalNameBinding, 0)

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "import_spec":
			symbols.CollectImportAlias(node, source, importAliases)
		case "function_declaration":
			symbols.CollectConstructorReturnType(node, source, constructorReturns)
			localNameBindings = append(localNameBindings, symbols.LocalNameBindingsFromParameters(node, source)...)
		case "method_declaration", "func_literal":
			localNameBindings = append(localNameBindings, symbols.LocalNameBindingsFromParameters(node, source)...)
		case "short_var_declaration":
			localNameBindings = append(
				localNameBindings,
				symbols.LocalNameBindingsFromNames(node, symbols.IdentifierNodes(node.ChildByFieldName("left"), source), source, lookup)...,
			)
		case "var_spec":
			localNameBindings = append(
				localNameBindings,
				symbols.LocalNameBindingsFromNames(node, symbols.IdentifierNodes(node.ChildByFieldName("name"), source), source, lookup)...,
			)
		}
	})

	return importAliases, constructorReturns, localNameBindings
}
