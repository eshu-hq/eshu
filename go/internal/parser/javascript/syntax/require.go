// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// RequireImportEntries returns one import record per binding a require declarator
// introduces, covering the plain, destructured, and property-of-module forms. It
// returns nil for a node that is not a variable_declarator, and for one whose value
// is not a statically resolvable require call.
func RequireImportEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "variable_declarator" {
		return nil
	}

	nameNode := node.ChildByFieldName("name")
	valueNode := node.ChildByFieldName("value")
	moduleSource, ok := RequireModuleSource(valueNode, source)
	requirePropertyName := ""
	if !ok {
		var propertyOK bool
		moduleSource, requirePropertyName, propertyOK = requireMemberModuleSource(valueNode, source)
		if !propertyOK {
			return nil
		}
	}

	fullImportName := fmt.Sprintf("const %s = require(%q)", strings.TrimSpace(shared.NodeText(nameNode, source)), moduleSource)
	lineNumber := shared.NodeLine(nameNode)

	switch nameNode.Kind() {
	case "identifier", "property_identifier", "private_property_identifier":
		localName := strings.TrimSpace(shared.NodeText(nameNode, source))
		if localName == "" {
			return nil
		}
		if requirePropertyName != "" {
			item := map[string]any{
				"name":             requirePropertyName,
				"source":           moduleSource,
				"import_type":      "require",
				"full_import_name": fullImportName,
				"line_number":      lineNumber,
				"lang":             lang,
			}
			if localName != requirePropertyName {
				item["alias"] = localName
			}
			return []map[string]any{item}
		}
		return []map[string]any{{
			"name":             "*",
			"alias":            localName,
			"source":           moduleSource,
			"import_type":      "require",
			"full_import_name": fullImportName,
			"line_number":      lineNumber,
			"lang":             lang,
		}}
	case "object_pattern":
		return requireObjectPatternEntries(nameNode, moduleSource, fullImportName, lineNumber, lang, source)
	default:
		return nil
	}
}

func requireMemberModuleSource(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "member_expression" {
		return "", "", false
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	moduleSource, ok := RequireModuleSource(objectNode, source)
	if !ok {
		return "", "", false
	}
	propertyName := IdentifierName(propertyNode, source)
	if propertyName == "" {
		return "", "", false
	}
	return moduleSource, propertyName, true
}

// RequireModuleSource returns the module specifier of a literal require call and
// whether one was found. It reports false for a computed, interpolated, or
// multi-argument call, none of which can be resolved without evaluating the
// program.
func RequireModuleSource(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "call_expression" {
		return "", false
	}

	functionNode := node.ChildByFieldName("function")
	if strings.TrimSpace(shared.NodeText(functionNode, source)) != "require" {
		return "", false
	}

	argumentsNode := node.ChildByFieldName("arguments")
	argumentsText := strings.TrimSpace(shared.NodeText(argumentsNode, source))
	if len(argumentsText) < 2 || argumentsText[0] != '(' || argumentsText[len(argumentsText)-1] != ')' {
		return "", false
	}

	argument := strings.TrimSpace(argumentsText[1 : len(argumentsText)-1])
	if argument == "" || strings.Contains(argument, ",") {
		return "", false
	}
	if strings.Contains(argument, "${") {
		return "", false
	}

	if unquoted, ok := TrimQuotes(argument); ok {
		return unquoted, true
	}
	return "", false
}

func requireObjectPatternEntries(
	nameNode *tree_sitter.Node,
	moduleSource string,
	fullImportName string,
	lineNumber int,
	lang string,
	source []byte,
) []map[string]any {
	if nameNode == nil {
		return nil
	}

	rawPattern := strings.TrimSpace(shared.NodeText(nameNode, source))
	if len(rawPattern) < 2 || rawPattern[0] != '{' || rawPattern[len(rawPattern)-1] != '}' {
		return nil
	}

	parts := strings.Split(rawPattern[1:len(rawPattern)-1], ",")
	items := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "..."))
		if part == "" {
			continue
		}

		ExportedName := part
		alias := ""
		if left, right, ok := strings.Cut(part, ":"); ok {
			ExportedName = strings.TrimSpace(left)
			alias = strings.TrimSpace(right)
		}
		ExportedName = strings.TrimSpace(ExportedName)
		if ExportedName == "" {
			continue
		}

		item := map[string]any{
			"name":             ExportedName,
			"source":           moduleSource,
			"import_type":      "require",
			"full_import_name": fullImportName,
			"line_number":      lineNumber,
			"lang":             lang,
		}
		if alias != "" && alias != ExportedName {
			item["alias"] = alias
		}
		items = append(items, item)
	}
	return items
}
