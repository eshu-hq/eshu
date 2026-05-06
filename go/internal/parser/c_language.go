package parser

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseC(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("c")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse c file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "c", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["unions"] = []map[string]any{}
	payload["macros"] = []map[string]any{}
	payload["typedefs"] = []map[string]any{}
	root := tree.RootNode()
	scope := options.normalizedVariableScope()

	walkNamed(root, func(node *tree_sitter.Node) {
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
			if strings.HasPrefix(strings.TrimSpace(nodeText(node, source)), "typedef ") {
				appendCTypedefAliases(payload, node, source, "c")
				return
			}
			if scope == "module" && cLikeInsideFunction(node) {
				return
			}
			appendCDeclarationVariables(payload, node, source, "c")
		case "call_expression":
			appendCCall(payload, node, source)
		}
	})
	appendCTypedefAliasesFromSource(payload, string(source), "c")

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

func (e *Engine) preScanC(path string) ([]string, error) {
	payload, err := e.parseC(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "structs", "enums", "unions", "macros", "typedefs")
	slices.Sort(names)
	return names, nil
}

var cTypedefAliasPattern = regexp.MustCompile(
	`(?s)typedef\s+(struct|enum|union)(?:\s+[A-Za-z_]\w*)?\s*\{.*?\}\s*([A-Za-z_]\w*)\s*;?`,
)

func appendCFunction(payload map[string]any, node *tree_sitter.Node, source []byte, options Options) {
	nameNode := firstNamedDescendant(node, "identifier", "field_identifier")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"decorators":  []string{},
		"lang":        "c",
	}
	if options.IndexSource {
		item["source"] = nodeText(node, source)
	}
	appendBucket(payload, "functions", item)
}

func appendCImportMetadata(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "system_lib_string", "string_literal")
	if nameNode == nil {
		return
	}
	name := strings.Trim(nodeText(nameNode, source), `<>"`)
	if name == "" {
		return
	}

	includeKind := "local"
	if nameNode.Kind() == "system_lib_string" {
		includeKind = "system"
	}

	appendBucket(payload, "imports", map[string]any{
		"name":             name,
		"source":           name,
		"full_import_name": strings.TrimSpace(nodeText(node, source)),
		"include_kind":     includeKind,
		"line_number":      nodeLine(node),
		"lang":             "c",
	})
}

func appendCTypedefAliases(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	bucket := ""
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		if specifierNode := firstNamedDescendant(typeNode, "struct_specifier", "enum_specifier", "union_specifier"); specifierNode != nil {
			switch specifierNode.Kind() {
			case "struct_specifier":
				bucket = "structs"
			case "enum_specifier":
				bucket = "enums"
			case "union_specifier":
				bucket = "unions"
			}
		}
		if bucket == "" {
			typeText := strings.TrimSpace(nodeText(typeNode, source))
			switch {
			case strings.HasPrefix(typeText, "struct"):
				bucket = "structs"
			case strings.HasPrefix(typeText, "enum"):
				bucket = "enums"
			case strings.HasPrefix(typeText, "union"):
				bucket = "unions"
			}
		}
	}
	if bucket == "" {
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			switch child.Kind() {
			case "struct_specifier":
				bucket = "structs"
			case "enum_specifier":
				bucket = "enums"
			case "union_specifier":
				bucket = "unions"
			}
			if bucket != "" {
				break
			}
		}
	}
	if bucket == "" {
		text := strings.TrimSpace(nodeText(node, source))
		matches := cTypedefAliasPattern.FindStringSubmatch(text)
		if len(matches) == 3 {
			bucket = map[string]string{
				"struct": "structs",
				"enum":   "enums",
				"union":  "unions",
			}[matches[1]]
		}
	}

	name := ""
	if declaratorNode := node.ChildByFieldName("declarator"); declaratorNode != nil {
		if nameNode := firstNamedDescendant(declaratorNode, "identifier", "type_identifier", "field_identifier"); nameNode != nil {
			name = strings.TrimSpace(nodeText(nameNode, source))
		}
		if name == "" {
			name = cTypedefAliasName(nodeText(declaratorNode, source))
		}
	}
	if name == "" {
		text := strings.TrimSpace(nodeText(node, source))
		matches := cTypedefAliasPattern.FindStringSubmatch(text)
		if len(matches) == 3 {
			name = strings.TrimSpace(matches[2])
		}
	}
	if name == "" {
		cursor := node.Walk()
		defer cursor.Close()
		seenBucketNode := false
		for _, child := range node.NamedChildren(cursor) {
			switch child.Kind() {
			case "struct_specifier", "enum_specifier", "union_specifier":
				seenBucketNode = true
			case "type_identifier", "identifier":
				if seenBucketNode {
					name = strings.TrimSpace(nodeText(&child, source))
				}
			}
		}
	}
	if name == "" {
		return
	}

	typedefItem := map[string]any{
		"name":        name,
		"line_number": nodeLine(node),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
		"type":        cTypedefUnderlyingType(node, source),
	}
	if !bucketContainsName(payload, "typedefs", name) {
		appendBucket(payload, "typedefs", typedefItem)
	}
	if bucket == "" || bucketContainsName(payload, bucket, name) {
		return
	}
	appendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": nodeLine(node),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
	})
}

func appendCTypedefAliasesFromSource(payload map[string]any, source string, lang string) {
	lines := strings.Split(source, "\n")
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		trimmed := strings.TrimSpace(lines[lineIndex])
		if !strings.HasPrefix(trimmed, "typedef ") {
			continue
		}
		bucket := ""
		switch {
		case strings.Contains(trimmed, "enum") && strings.Contains(trimmed, "{"):
			bucket = "enums"
		case strings.Contains(trimmed, "struct") && strings.Contains(trimmed, "{"):
			bucket = "structs"
		case strings.Contains(trimmed, "union") && strings.Contains(trimmed, "{"):
			bucket = "unions"
		}
		if bucket == "" {
			continue
		}
		block := trimmed
		endIndex := lineIndex
		for !strings.Contains(block, "}") && endIndex+1 < len(lines) {
			endIndex++
			block += " " + strings.TrimSpace(lines[endIndex])
		}
		if !strings.Contains(block, ";") {
			for endIndex+1 < len(lines) && !strings.Contains(block, ";") {
				endIndex++
				block += " " + strings.TrimSpace(lines[endIndex])
			}
		}
		if !strings.Contains(block, ";") {
			continue
		}
		aliasPart := strings.TrimSpace(block[strings.LastIndex(block, "}")+1:])
		aliasPart = strings.TrimSuffix(aliasPart, ";")
		name := cTypedefAliasName(aliasPart)
		if name == "" {
			continue
		}
		if !bucketContainsName(payload, "typedefs", name) {
			appendBucket(payload, "typedefs", map[string]any{
				"name":        name,
				"line_number": lineIndex + 1,
				"end_line":    endIndex + 1,
				"lang":        lang,
				"type":        cTypedefUnderlyingTypeFromBlock(block),
			})
		}
		if bucketContainsName(payload, bucket, name) {
			continue
		}
		appendBucket(payload, bucket, map[string]any{
			"name":        name,
			"line_number": lineIndex + 1,
			"end_line":    endIndex + 1,
			"lang":        lang,
		})
	}
}

func appendCCall(payload map[string]any, node *tree_sitter.Node, source []byte) {
	functionNode := node.ChildByFieldName("function")
	nameNode := cLikeCallNameNode(functionNode)
	if nameNode == nil {
		return
	}

	name := strings.TrimSpace(nodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"lang":        "c",
	}
	if fullName := cCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	appendBucket(payload, "function_calls", item)
}

func bucketContainsName(payload map[string]any, bucket string, name string) bool {
	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		existing, _ := item["name"].(string)
		if strings.TrimSpace(existing) == name {
			return true
		}
	}
	return false
}

func cTypedefAliasName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)
	if idx := strings.LastIndex(trimmed, "]"); idx >= 0 {
		trimmed = trimmed[:idx+1]
	}
	fields := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r != '_' &&
			(r < 'A' || r > 'Z') &&
			(r < 'a' || r > 'z') &&
			(r < '0' || r > '9')
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func cTypedefUnderlyingType(node *tree_sitter.Node, source []byte) string {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(typeNode, source))
}

func cCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(functionNode, source))
}

func cTypedefUnderlyingTypeFromBlock(block string) string {
	trimmed := strings.TrimSpace(block)
	trimmed = strings.TrimPrefix(trimmed, "typedef")
	if aliasIndex := strings.LastIndex(trimmed, "}"); aliasIndex >= 0 {
		return strings.TrimSpace(trimmed[:aliasIndex+1])
	}
	if semicolonIndex := strings.LastIndex(trimmed, ";"); semicolonIndex >= 0 {
		trimmed = strings.TrimSpace(trimmed[:semicolonIndex])
	}
	parts := strings.Fields(trimmed)
	if len(parts) <= 1 {
		return strings.TrimSpace(trimmed)
	}
	return strings.Join(parts[:len(parts)-1], " ")
}

func appendCDeclarationVariables(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "init_declarator" {
			return
		}
		nameNode := firstNamedDescendant(child, "identifier")
		name := nodeText(nameNode, source)
		if strings.TrimSpace(name) == "" {
			return
		}
		appendBucket(payload, "variables", map[string]any{
			"name":        name,
			"line_number": nodeLine(nameNode),
			"end_line":    nodeEndLine(node),
			"lang":        lang,
		})
	})
}
