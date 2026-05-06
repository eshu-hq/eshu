package parser

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseCSharp(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("c_sharp")
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
		return nil, fmt.Errorf("parse c# file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "c_sharp", isDependency)
	payload["interfaces"] = []map[string]any{}
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["records"] = []map[string]any{}
	payload["properties"] = []map[string]any{}
	root := tree.RootNode()

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_declaration":
			appendCSharpNamedType(payload, "classes", node, source)
		case "interface_declaration":
			appendCSharpNamedType(payload, "interfaces", node, source)
		case "struct_declaration":
			appendCSharpNamedType(payload, "structs", node, source)
		case "enum_declaration":
			appendCSharpNamedType(payload, "enums", node, source)
		case "record_declaration":
			appendCSharpNamedType(payload, "records", node, source)
		case "property_declaration":
			appendNamedType(payload, "properties", node, source, "c_sharp")
		case "method_declaration", "constructor_declaration", "local_function_statement":
			appendFunctionWithContext(
				payload,
				node,
				source,
				"c_sharp",
				options,
				"class_declaration",
				"interface_declaration",
				"struct_declaration",
				"record_declaration",
			)
		case "using_directive":
			name := csharpUsingName(node, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "imports", map[string]any{
				"name":        name,
				"line_number": nodeLine(node),
				"lang":        "c_sharp",
			})
		case "invocation_expression":
			functionNode := node.ChildByFieldName("function")
			appendCall(payload, csharpCallNameNode(functionNode), source, "c_sharp")
		case "object_creation_expression":
			appendCall(payload, csharpObjectCreationNameNode(node), source, "c_sharp")
		}
	})

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "interfaces")
	sortNamedBucket(payload, "structs")
	sortNamedBucket(payload, "enums")
	sortNamedBucket(payload, "records")
	sortNamedBucket(payload, "properties")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

func (e *Engine) preScanCSharp(path string) ([]string, error) {
	payload, err := e.parseCSharp(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "interfaces", "structs", "records")
	slices.Sort(names)
	return names, nil
}

func csharpUsingName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	var parts []string
	for _, child := range node.NamedChildren(cursor) {
		child := child
		parts = append(parts, nodeText(&child, source))
	}
	return strings.Join(parts, ".")
}

func csharpCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "identifier":
		return node
	case "member_access_expression":
		return node.ChildByFieldName("name")
	default:
		return csharpFirstIdentifier(node)
	}
}

func csharpObjectCreationNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if nameNode := node.ChildByFieldName("type"); nameNode != nil {
		return nameNode
	}
	return csharpFirstIdentifier(node)
}

func csharpFirstIdentifier(node *tree_sitter.Node) *tree_sitter.Node {
	var result *tree_sitter.Node
	walkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		switch child.Kind() {
		case "identifier", "qualified_name", "generic_name":
			result = cloneNode(child)
		}
	})
	return result
}

func appendCSharpNamedType(
	payload map[string]any,
	bucket string,
	node *tree_sitter.Node,
	source []byte,
) {
	nameNode := node.ChildByFieldName("name")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        "c_sharp",
	}
	if bases := csharpBaseNames(node, source); len(bases) > 0 {
		item["bases"] = bases
	}
	appendBucket(payload, bucket, item)
}

func csharpBaseNames(node *tree_sitter.Node, source []byte) []string {
	baseListNode := csharpBaseListNode(node)
	if baseListNode == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var bases []string
	walkNamed(baseListNode, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "identifier", "qualified_name", "generic_name":
		default:
			return
		}
		name := strings.TrimSpace(nodeText(child, source))
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		bases = append(bases, name)
	})
	slices.Sort(bases)
	return bases
}

func csharpBaseListNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if baseListNode := node.ChildByFieldName("bases"); baseListNode != nil {
		return baseListNode
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "base_list" {
			return cloneNode(&child)
		}
	}
	return nil
}
