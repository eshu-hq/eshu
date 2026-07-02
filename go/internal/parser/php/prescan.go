// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func preScanNames(path string, parser *tree_sitter.Parser) ([]string, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, parseError(path)
	}
	defer tree.Close()

	names := phpPreScanNames(tree.RootNode(), source)
	slices.Sort(names)
	return names, nil
}

func phpPreScanNames(root *tree_sitter.Node, source []byte) []string {
	var names []string
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration",
			"function_definition", "method_declaration":
			names = appendPHPPreScanName(names, phpDeclarationName(node, source))
		case "anonymous_class":
			names = appendPHPPreScanName(names, phpAnonymousClassName(shared.NodeLine(node)))
		}
	})
	return names
}

func appendPHPPreScanName(names []string, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return names
	}
	return append(names, filepath.Clean(name))
}
