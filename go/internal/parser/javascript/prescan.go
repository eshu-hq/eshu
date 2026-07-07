// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"log/slog"
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

	// jsParseByteCap (Parse's over-cap bound, #4766) applies here too: pre-scan
	// runs across the FULL repository on every delta sync, unlike the normal
	// parse stage which only visits changed targets, so an over-cap file would
	// otherwise still pay the same superlinear tree-sitter cost in this stage.
	// A bounded file contributes no pre-scan names, mirroring Parse's bounded
	// (empty) payload for the same file.
	if len(source) > jsParseByteCap {
		recordJSPreScanBoundedFile(path, len(source))
		return nil, nil
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

// recordJSPreScanBoundedFile logs one file whose size exceeded jsParseByteCap
// and whose pre-scan tree-sitter parse was skipped entirely, mirroring
// recordJSBoundedFile's structured log line for the normal Parse stage so a
// dropped pre-scan is observable rather than silent. Pre-scan has no payload
// map to record a js_parse_bounded row against (it returns only a name
// slice), so the structured log is the sole observability signal here.
func recordJSPreScanBoundedFile(path string, originalBytes int) {
	slog.Warn(
		"javascript-family pre-scan file bounded",
		"component", "parser.javascript",
		"path", path,
		"original_bytes", originalBytes,
		"action", "file_skipped",
	)
}
