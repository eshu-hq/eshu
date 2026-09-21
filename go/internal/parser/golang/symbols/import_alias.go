// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"path"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImportAliasIndex maps each import path in root to the aliases the file
// binds it to. It is a standalone full-tree walk used by package-prescan
// callers; ImportAlias applies the identical alias-extraction logic used by
// the per-file merged walk in Parse.
func ImportAliasIndex(root *tree_sitter.Node, source []byte) map[string][]string {
	index := make(map[string][]string)
	if root == nil {
		return index
	}

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_spec" {
			return
		}
		CollectImportAlias(node, source, index)
	})
	return index
}

// CollectImportAlias records the import alias for one import_spec node into
// index, if any. It is the single-node visitor shared by ImportAliasIndex
// (a standalone full-tree walk used by package-prescan callers) and the
// per-file merged walk in Parse, so both paths apply identical import-alias
// extraction logic.
func CollectImportAlias(node *tree_sitter.Node, source []byte, index map[string][]string) {
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		return
	}
	importPath := strings.TrimSpace(strings.Trim(shared.NodeText(pathNode, source), `"`))
	if importPath == "" {
		return
	}

	alias := ImportAlias(node, source, importPath)
	if alias == "" || alias == "." || alias == "_" {
		return
	}
	index[importPath] = AppendUniqueImportAlias(index[importPath], alias)
}

// ImportAlias returns the alias one import_spec node binds importPath to,
// falling back to the import path's base name when the spec has no explicit
// alias.
func ImportAlias(node *tree_sitter.Node, source []byte, importPath string) string {
	if node == nil {
		return ""
	}
	if aliasNode := node.ChildByFieldName("name"); aliasNode != nil {
		if alias := strings.TrimSpace(shared.NodeText(aliasNode, source)); alias != "" {
			return alias
		}
	}
	return path.Base(importPath)
}

// AliasesForImportPath returns a sorted copy of the aliases index records for
// importPath.
func AliasesForImportPath(index map[string][]string, importPath string) []string {
	aliases := append([]string(nil), index[importPath]...)
	slices.Sort(aliases)
	return aliases
}

// AppendUniqueImportAlias appends value to values if it is not already
// present, preserving insertion order.
func AppendUniqueImportAlias(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
