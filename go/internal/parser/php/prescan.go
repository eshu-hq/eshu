// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"log/slog"
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

	// phpParseByteCap (Parse's over-cap bound, #4766) applies here too:
	// pre-scan runs across the FULL repository on every delta sync, unlike the
	// normal parse stage which only visits changed targets, so an over-cap
	// file would otherwise still pay the same superlinear tree-sitter cost in
	// this stage. A bounded file contributes no pre-scan names, mirroring
	// Parse's bounded (empty) payload for the same file.
	if len(source) > phpParseByteCap {
		recordPHPPreScanBoundedFile(path, len(source))
		return nil, nil
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

// recordPHPPreScanBoundedFile logs one file whose size exceeded
// phpParseByteCap and whose pre-scan tree-sitter parse was skipped entirely,
// mirroring recordPHPBoundedFile's structured log line for the normal Parse
// stage so a dropped pre-scan is observable rather than silent. Pre-scan has
// no payload map to record a php_parse_bounded row against (it returns only a
// name slice), so the structured log is the sole observability signal here.
func recordPHPPreScanBoundedFile(path string, originalBytes int) {
	slog.Warn(
		"php pre-scan file bounded",
		"component", "parser.php",
		"path", path,
		"original_bytes", originalBytes,
		"action", "file_skipped",
	)
}
