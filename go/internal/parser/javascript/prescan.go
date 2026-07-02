// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func preScanNames(
	parserFactory ParserFactory,
	parserReturner ParserReturner,
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	parser, err := parserFactory(runtimeLanguage)
	if err != nil {
		return nil, err
	}
	defer parserReturner(runtimeLanguage, parser)

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s file %q: parser returned nil tree", outputLanguage, path)
	}
	defer tree.Close()

	names := javaScriptPreScanNames(tree.RootNode(), source, outputLanguage)
	slices.Sort(names)
	return names, nil
}

func javaScriptPreScanNames(root *tree_sitter.Node, source []byte, outputLanguage string) []string {
	var names []string
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration", "method_definition":
			names = appendPreScanName(names, node.ChildByFieldName("name"), source)
		case "class_declaration", "abstract_class_declaration":
			names = appendPreScanName(names, node.ChildByFieldName("name"), source)
		case "interface_declaration":
			if outputLanguage != "javascript" {
				names = appendPreScanName(names, node.ChildByFieldName("name"), source)
			}
		case "variable_declarator":
			if isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
				names = appendPreScanName(names, node.ChildByFieldName("name"), source)
			}
		case "pair":
			if isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
				names = appendPreScanName(names, node.ChildByFieldName("key"), source)
			}
		case "assignment_expression":
			if !isJavaScriptFunctionValue(node.ChildByFieldName("right")) {
				return
			}
			names = appendPreScanName(names, javaScriptExportAssignmentNameNode(node.ChildByFieldName("left"), source), source)
		}
	})
	return names
}

func appendPreScanName(names []string, node *tree_sitter.Node, source []byte) []string {
	name := strings.TrimSpace(javaScriptFunctionName(node, source))
	if name == "" {
		return names
	}
	return append(names, filepath.Clean(name))
}
