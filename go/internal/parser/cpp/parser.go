package cpp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var cTypedefAliasPattern = regexp.MustCompile(
	`(?s)typedef\s+(struct|enum|union)(?:\s+[A-Za-z_]\w*)?\s*\{.*?\}\s*([A-Za-z_]\w*)\s*;?`,
)

// Parse reads and parses a C++ file using a caller-owned tree-sitter parser.
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
		return nil, fmt.Errorf("parse c++ file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "cpp", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["unions"] = []map[string]any{}
	payload["macros"] = []map[string]any{}
	root := tree.RootNode()

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "preproc_include":
			appendImportFromNode(payload, firstNamedDescendant(node, "system_lib_string", "string_literal"), source, "cpp")
		case "preproc_def", "preproc_function_def":
			appendMacro(payload, node, source, "cpp")
		case "class_specifier":
			appendNamedType(payload, "classes", node, source, "cpp")
		case "struct_specifier":
			appendNamedType(payload, "structs", node, source, "cpp")
		case "enum_specifier":
			appendNamedType(payload, "enums", node, source, "cpp")
		case "union_specifier":
			appendNamedType(payload, "unions", node, source, "cpp")
		case "type_definition":
			appendCTypedefAliases(payload, node, source, "cpp")
		case "function_definition":
			appendCPPFunction(payload, node, source, options)
		case "declaration":
			appendCTypedefAliases(payload, node, source, "cpp")
		case "call_expression":
			appendCall(payload, cLikeCallNameNode(node.ChildByFieldName("function")), source, "cpp")
		}
	})
	appendCTypedefAliasesFromSource(payload, string(source), "cpp")

	sortSystemsPayload(
		payload,
		"functions",
		"classes",
		"structs",
		"enums",
		"unions",
		"imports",
		"function_calls",
		"macros",
	)
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

// PreScan returns named C++ symbols used by dependency pre-scanning.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	return shared.CollectBucketNames(payload, "functions", "classes", "structs", "enums", "unions", "macros"), nil
}

func appendCPPFunction(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	nameNode := firstNamedDescendant(node, "identifier", "field_identifier")
	name := shared.NodeText(nameNode, source)
	if name == "" {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"decorators":  []string{},
		"lang":        "cpp",
	}
	if classContext := nearestNamedAncestor(node, source, "class_specifier", "struct_specifier"); classContext != "" {
		item["class_context"] = classContext
	}
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "functions", item)
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
	if matches := cTypedefAliasPattern.FindStringSubmatch(strings.TrimSpace(shared.NodeText(node, source))); len(matches) == 3 {
		return map[string]string{"struct": "structs", "enum": "enums", "union": "unions"}[matches[1]]
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
	text := strings.TrimSpace(shared.NodeText(node, source))
	if matches := cTypedefAliasPattern.FindStringSubmatch(text); len(matches) == 3 {
		return strings.TrimSpace(matches[2])
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
			shared.AppendBucket(payload, "typedefs", map[string]any{
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
		shared.AppendBucket(payload, bucket, map[string]any{
			"name":        name,
			"line_number": lineIndex + 1,
			"end_line":    endIndex + 1,
			"lang":        lang,
		})
	}
}
