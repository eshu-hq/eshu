package javascript

import (
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptRequireImportEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "variable_declarator" {
		return nil
	}

	nameNode := node.ChildByFieldName("name")
	valueNode := node.ChildByFieldName("value")
	moduleSource, ok := javaScriptRequireModuleSource(valueNode, source)
	requirePropertyName := ""
	if !ok {
		var propertyOK bool
		moduleSource, requirePropertyName, propertyOK = javaScriptRequireMemberModuleSource(valueNode, source)
		if !propertyOK {
			return nil
		}
	}

	fullImportName := fmt.Sprintf("const %s = require(%q)", strings.TrimSpace(nodeText(nameNode, source)), moduleSource)
	lineNumber := nodeLine(nameNode)

	switch nameNode.Kind() {
	case "identifier", "property_identifier", "private_property_identifier":
		localName := strings.TrimSpace(nodeText(nameNode, source))
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
		return javaScriptRequireObjectPatternEntries(nameNode, moduleSource, fullImportName, lineNumber, lang, source)
	default:
		return nil
	}
}

func javaScriptRequireMemberModuleSource(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "member_expression" {
		return "", "", false
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	moduleSource, ok := javaScriptRequireModuleSource(objectNode, source)
	if !ok {
		return "", "", false
	}
	propertyName := javaScriptIdentifierName(propertyNode, source)
	if propertyName == "" {
		return "", "", false
	}
	return moduleSource, propertyName, true
}

func javaScriptRequireModuleSource(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "call_expression" {
		return "", false
	}

	functionNode := node.ChildByFieldName("function")
	if strings.TrimSpace(nodeText(functionNode, source)) != "require" {
		return "", false
	}

	argumentsNode := node.ChildByFieldName("arguments")
	argumentsText := strings.TrimSpace(nodeText(argumentsNode, source))
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

	if unquoted, ok := trimJavaScriptQuotes(argument); ok {
		return unquoted, true
	}
	return "", false
}

func javaScriptRequireObjectPatternEntries(
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

	rawPattern := strings.TrimSpace(nodeText(nameNode, source))
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

		exportedName := part
		alias := ""
		if left, right, ok := strings.Cut(part, ":"); ok {
			exportedName = strings.TrimSpace(left)
			alias = strings.TrimSpace(right)
		}
		exportedName = strings.TrimSpace(exportedName)
		if exportedName == "" {
			continue
		}

		item := map[string]any{
			"name":             exportedName,
			"source":           moduleSource,
			"import_type":      "require",
			"full_import_name": fullImportName,
			"line_number":      lineNumber,
			"lang":             lang,
		}
		if alias != "" && alias != exportedName {
			item["alias"] = alias
		}
		items = append(items, item)
	}
	return items
}
