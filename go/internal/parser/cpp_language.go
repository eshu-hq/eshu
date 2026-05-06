package parser

import (
	"fmt"
	"slices"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseCPP(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("cpp")
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
		return nil, fmt.Errorf("parse c++ file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "cpp", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["unions"] = []map[string]any{}
	payload["macros"] = []map[string]any{}
	root := tree.RootNode()

	walkNamed(root, func(node *tree_sitter.Node) {
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

func (e *Engine) preScanCPP(path string) ([]string, error) {
	payload, err := e.parseCPP(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "structs", "enums", "unions", "macros")
	slices.Sort(names)
	return names, nil
}

func appendCPPFunction(payload map[string]any, node *tree_sitter.Node, source []byte, options Options) {
	nameNode := firstNamedDescendant(node, "identifier", "field_identifier")
	name := nodeText(nameNode, source)
	if name == "" {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"decorators":  []string{},
		"lang":        "cpp",
	}
	if classContext := nearestNamedAncestor(node, source, "class_specifier", "struct_specifier"); classContext != "" {
		item["class_context"] = classContext
	}
	if options.IndexSource {
		item["source"] = nodeText(node, source)
	}
	appendBucket(payload, "functions", item)
}
