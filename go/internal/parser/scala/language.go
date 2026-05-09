package scala

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Scala declarations, imports, variables, and calls.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse scala file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "scala", isDependency)
	payload["traits"] = []map[string]any{}
	root := tree.RootNode()
	scope := options.NormalizedVariableScope()

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
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
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"lang":        "scala",
			})
		case "call_expression":
			appendCall(payload, scalaCallNameNode(node), source, "scala")
		}
	})

	for _, bucket := range []string{"functions", "classes", "traits", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

// PreScan returns Scala names used by the collector import-map pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "traits")
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
		name := shared.NodeText(&child, source)
		if strings.TrimSpace(name) == "" {
			continue
		}
		shared.AppendBucket(payload, "variables", map[string]any{
			"name":        name,
			"line_number": shared.NodeLine(&child),
			"end_line":    shared.NodeEndLine(node),
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
		parts = append(parts, shared.NodeText(&child, source))
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
			return shared.CloneNode(&child)
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
		"decorators":  []string{},
		"lang":        lang,
	}
	if classContext := nearestNamedAncestor(node, source, contextKinds...); classContext != "" {
		item["class_context"] = classContext
	}
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "functions", item)
}

func nearestNamedAncestor(node *tree_sitter.Node, source []byte, kinds ...string) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		for _, kind := range kinds {
			if current.Kind() != kind {
				continue
			}
			nameNode := current.ChildByFieldName("name")
			return shared.NodeText(nameNode, source)
		}
	}
	return ""
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
