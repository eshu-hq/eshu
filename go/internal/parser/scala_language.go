package parser

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseScala(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("scala")
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
		return nil, fmt.Errorf("parse scala file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "scala", isDependency)
	payload["traits"] = []map[string]any{}
	root := tree.RootNode()
	scope := options.normalizedVariableScope()

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_definition", "object_definition":
			appendNamedType(payload, "classes", node, source, "scala")
		case "trait_definition":
			appendNamedType(payload, "traits", node, source, "scala")
		case "function_definition", "function_declaration":
			appendFunctionWithContext(
				payload,
				node,
				source,
				"scala",
				options,
				"class_definition",
				"object_definition",
				"trait_definition",
			)
		case "val_definition", "var_definition":
			if scope == "module" && scalaInsideFunction(node) {
				return
			}
			appendScalaVariables(payload, node, source)
		case "import_declaration":
			name := scalaImportName(node, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "imports", map[string]any{
				"name":        name,
				"line_number": nodeLine(node),
				"lang":        "scala",
			})
		case "call_expression":
			appendCall(payload, scalaCallNameNode(node), source, "scala")
		}
	})

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "traits")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

func (e *Engine) preScanScala(path string) ([]string, error) {
	payload, err := e.parseScala(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "traits")
	slices.Sort(names)
	return names, nil
}

func appendScalaVariables(payload map[string]any, node *tree_sitter.Node, source []byte) {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" {
			continue
		}
		name := nodeText(&child, source)
		if strings.TrimSpace(name) == "" {
			continue
		}
		appendBucket(payload, "variables", map[string]any{
			"name":        name,
			"line_number": nodeLine(&child),
			"end_line":    nodeEndLine(node),
			"lang":        "scala",
		})
	}
}

func scalaImportName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	var parts []string
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" {
			continue
		}
		parts = append(parts, nodeText(&child, source))
	}
	return strings.Join(parts, ".")
}

func scalaCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "identifier", "generic_function":
			return cloneNode(&child)
		}
	}
	return nil
}

func scalaInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "function_declaration":
			return true
		}
	}
	return false
}
