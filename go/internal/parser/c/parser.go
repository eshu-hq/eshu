// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package c

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse reads and parses a C file using a caller-owned tree-sitter parser.
func Parse(
	path string,
	isDependency bool,
	options shared.Options,
	parser *tree_sitter.Parser,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse c file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "c", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["unions"] = []map[string]any{}
	payload["macros"] = []map[string]any{}
	payload["typedefs"] = []map[string]any{}
	root := tree.RootNode()
	scope := options.NormalizedVariableScope()

	// Gather dead-code-root resolution-candidate node pointers during the
	// main walk in a single ordered slice (pre-order) so
	// annotateCDeadCodeRoots can resolve them in one in-memory loop instead
	// of a second full-tree shared.WalkNamed traversal. Appending to one
	// slice during the main walk's pre-order preserves the original
	// interleaved visitation order of the old second walk exactly (a
	// declaration before a call when the declaration appears earlier in
	// source), which is load-bearing for byte-identical dead_code_root_kinds
	// slice ordering (issue #4870, following the pattern fixed for C++ in
	// #4844/#4924). Node pointers are cloned (shared.CloneNode) for safe
	// retention past the walk callback.
	var gatheredResolutionNodes []*tree_sitter.Node

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "preproc_include":
			appendCImportMetadata(payload, node, source)
		case "preproc_def", "preproc_function_def":
			appendMacro(payload, node, source, "c")
		case "struct_specifier":
			appendNamedType(payload, "structs", node, source, "c")
		case "enum_specifier":
			appendNamedType(payload, "enums", node, source, "c")
		case "union_specifier":
			appendNamedType(payload, "unions", node, source, "c")
		case "type_definition":
			appendCTypedefAliases(payload, node, source, "c")
		case "function_definition":
			appendCFunction(payload, node, source, options)
		case "declaration":
			gatheredResolutionNodes = append(gatheredResolutionNodes, shared.CloneNode(node))
			if strings.HasPrefix(strings.TrimSpace(shared.NodeText(node, source)), "typedef ") {
				appendCTypedefAliases(payload, node, source, "c")
				return
			}
			if scope == "module" && cLikeInsideFunction(node) {
				return
			}
			appendCDeclarationVariables(payload, node, source, "c")
		case "call_expression":
			appendCCall(payload, node, source)
			gatheredResolutionNodes = append(gatheredResolutionNodes, shared.CloneNode(node))
		}
	})
	annotateCDeadCodeRoots(payload, source, gatheredResolutionNodes)

	sortSystemsPayload(
		payload,
		"functions",
		"structs",
		"enums",
		"unions",
		"variables",
		"imports",
		"function_calls",
		"macros",
		"typedefs",
	)
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

// PreScan returns named C symbols used by dependency pre-scanning.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	return shared.CollectBucketNames(payload, "functions", "structs", "enums", "unions", "macros", "typedefs"), nil
}

func appendCFunction(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	nameNode := firstNamedDescendant(node, "identifier", "field_identifier")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	item := map[string]any{
		"name":                  name,
		"line_number":           shared.NodeLine(nameNode),
		"end_line":              shared.NodeEndLine(node),
		"decorators":            []string{},
		"lang":                  "c",
		"cyclomatic_complexity": cyclomaticComplexity(node, source),
	}
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "functions", item)
}

func appendCImportMetadata(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "system_lib_string", "string_literal")
	if nameNode == nil {
		return
	}
	name := strings.Trim(shared.NodeText(nameNode, source), `<>"`)
	if name == "" {
		return
	}

	includeKind := "local"
	if nameNode.Kind() == "system_lib_string" {
		includeKind = "system"
	}

	shared.AppendBucket(payload, "imports", map[string]any{
		"name":             name,
		"source":           name,
		"full_import_name": strings.TrimSpace(shared.NodeText(node, source)),
		"include_kind":     includeKind,
		"line_number":      shared.NodeLine(node),
		"lang":             "c",
	})
}

func appendCTypedefAliases(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	bucket := cTypedefBucket(node, source)
	name := cTypedefName(node, source)
	if name == "" {
		return
	}

	typedefItem := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        lang,
		"type":        cTypedefUnderlyingType(node, source),
	}
	if !bucketContainsName(payload, "typedefs", name) {
		shared.AppendBucket(payload, "typedefs", typedefItem)
	}
	if bucket == "" || bucketContainsName(payload, bucket, name) {
		return
	}
	shared.AppendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        lang,
	})
}

func cTypedefBucket(node *tree_sitter.Node, source []byte) string {
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		if specifierNode := firstNamedDescendant(typeNode, "struct_specifier", "enum_specifier", "union_specifier"); specifierNode != nil {
			switch specifierNode.Kind() {
			case "struct_specifier":
				return "structs"
			case "enum_specifier":
				return "enums"
			case "union_specifier":
				return "unions"
			}
		}
		typeText := strings.TrimSpace(shared.NodeText(typeNode, source))
		switch {
		case strings.HasPrefix(typeText, "struct"):
			return "structs"
		case strings.HasPrefix(typeText, "enum"):
			return "enums"
		case strings.HasPrefix(typeText, "union"):
			return "unions"
		}
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case "struct_specifier":
			return "structs"
		case "enum_specifier":
			return "enums"
		case "union_specifier":
			return "unions"
		}
	}
	return ""
}

func cTypedefName(node *tree_sitter.Node, source []byte) string {
	if declaratorNode := node.ChildByFieldName("declarator"); declaratorNode != nil {
		if nameNode := firstNamedDescendant(declaratorNode, "identifier", "type_identifier", "field_identifier"); nameNode != nil {
			if name := strings.TrimSpace(shared.NodeText(nameNode, source)); name != "" {
				return name
			}
		}
		if name := cTypedefAliasName(shared.NodeText(declaratorNode, source)); name != "" {
			return name
		}
	}
	cursor := node.Walk()
	defer cursor.Close()
	seenBucketNode := false
	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case "struct_specifier", "enum_specifier", "union_specifier":
			seenBucketNode = true
		case "type_identifier", "identifier":
			if seenBucketNode {
				return strings.TrimSpace(shared.NodeText(&child, source))
			}
		}
	}
	return ""
}

func appendCCall(payload map[string]any, node *tree_sitter.Node, source []byte) {
	functionNode := node.ChildByFieldName("function")
	nameNode := cLikeCallNameNode(functionNode)
	if nameNode == nil {
		return
	}

	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"lang":        "c",
	}
	if fullName := cCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	shared.AppendBucket(payload, "function_calls", item)
}

func appendCDeclarationVariables(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "init_declarator" {
			return
		}
		nameNode := firstNamedDescendant(child, "identifier")
		name := shared.NodeText(nameNode, source)
		if strings.TrimSpace(name) == "" {
			return
		}
		shared.AppendBucket(payload, "variables", map[string]any{
			"name":        name,
			"line_number": shared.NodeLine(nameNode),
			"end_line":    shared.NodeEndLine(node),
			"lang":        lang,
		})
	})
}

func cLikeInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "function_definition" {
			return true
		}
	}
	return false
}
