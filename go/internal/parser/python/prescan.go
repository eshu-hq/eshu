// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func preScanNames(path string, parser *tree_sitter.Parser) ([]string, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".ipynb") {
		source, err = notebookPythonSource(path, source)
		if err != nil {
			return nil, err
		}
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse python file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	names := pythonPreScanNames(path, tree.RootNode(), source)
	slices.Sort(names)
	return names, nil
}

func pythonPreScanNames(path string, root *tree_sitter.Node, source []byte) []string {
	var names []string
	if docstring := pythonDocstring(root, source); docstring != "" {
		moduleName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if moduleName == "" {
			moduleName = filepath.Base(path)
		}
		names = appendPreScanName(names, moduleName)
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_definition", "function_definition":
			names = appendPreScanName(names, nodeText(node.ChildByFieldName("name"), source))
		}
	})
	return names
}

func appendPreScanName(names []string, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return names
	}
	return append(names, filepath.Clean(name))
}
