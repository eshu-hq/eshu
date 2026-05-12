package csharp

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts C# declarations, imports, calls, and inheritance metadata.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse c# file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "c_sharp", isDependency)
	payload["interfaces"] = []map[string]any{}
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["records"] = []map[string]any{}
	payload["properties"] = []map[string]any{}
	root := tree.RootNode()
	facts := collectCSharpSemanticFacts(root, source)

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
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
				facts,
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
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"lang":        "c_sharp",
			})
		case "invocation_expression":
			functionNode := node.ChildByFieldName("function")
			appendCall(payload, csharpCallNameNode(functionNode), source, "c_sharp")
		case "object_creation_expression":
			appendCall(payload, csharpObjectCreationNameNode(node), source, "c_sharp")
		}
	})

	for _, bucket := range []string{
		"functions", "classes", "interfaces", "structs", "enums",
		"records", "properties", "imports", "function_calls",
	} {
		shared.SortNamedBucket(payload, bucket)
	}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

// PreScan returns C# names used by the collector import-map pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "interfaces", "structs", "records")
	slices.Sort(names)
	return names, nil
}

func csharpUsingName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	var parts []string
	for _, child := range node.NamedChildren(cursor) {
		child := child
		parts = append(parts, shared.NodeText(&child, source))
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
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		switch child.Kind() {
		case "identifier", "qualified_name", "generic_name":
			result = shared.CloneNode(child)
		}
	})
	return result
}

func appendCSharpNamedType(payload map[string]any, bucket string, node *tree_sitter.Node, source []byte) {
	nameNode := node.ChildByFieldName("name")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "c_sharp",
	}
	if bases := csharpBaseNames(node, source); len(bases) > 0 {
		item["bases"] = bases
	}
	shared.AppendBucket(payload, bucket, item)
}

func appendNamedType(payload map[string]any, bucket string, node *tree_sitter.Node, source []byte, lang string) {
	nameNode := node.ChildByFieldName("name")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	shared.AppendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        lang,
	})
}

func appendFunctionWithContext(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	lang string,
	options shared.Options,
	facts csharpSemanticFacts,
	contextKinds ...string,
) {
	nameNode := node.ChildByFieldName("name")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"decorators":  csharpAttributeNames(node, source),
		"lang":        lang,
	}
	contextName, contextKind := nearestNamedAncestorWithKind(node, source, contextKinds...)
	if contextName != "" {
		item["class_context"] = contextName
	}
	if rootKinds := csharpFunctionRootKinds(node, source, name, contextName, contextKind, facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "functions", item)
}

func appendCall(payload map[string]any, nameNode *tree_sitter.Node, source []byte, lang string) {
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"lang":        lang,
	})
}
