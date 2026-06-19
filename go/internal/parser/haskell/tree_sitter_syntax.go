package haskell

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type haskellFunctionSpan struct {
	name      string
	params    map[string]struct{}
	source    string
	startLine int
	endLine   int
}

type haskellSyntaxIndex struct {
	functions []haskellFunctionSpan
}

func haskellSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, haskellSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, haskellSyntaxIndex{}, err
	}
	if parser == nil {
		return nil, haskellSyntaxIndex{}, fmt.Errorf("parse haskell tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, haskellSyntaxIndex{}, fmt.Errorf("parse haskell tree: parser returned nil tree")
	}
	defer tree.Close()

	lines := strings.Split(string(source), "\n")
	index := haskellSyntaxIndex{}
	index.collect(tree.RootNode(), source, lines)
	return source, index, nil
}

func (i *haskellSyntaxIndex) collect(node *tree_sitter.Node, source []byte, lines []string) {
	if node == nil {
		return
	}
	if node.Kind() == "function" {
		if !haskellTreeFunctionInDeclarationScope(node) {
			return
		}
		if fn := haskellFunctionFromTree(node, source, lines); fn.name != "" {
			i.functions = append(i.functions, fn)
		}
		return
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		i.collect(&child, source, lines)
	}
}

func haskellTreeFunctionInDeclarationScope(node *tree_sitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		switch parent.Kind() {
		case "let", "let_in", "local_binds", "where", "where_clause", "expression", "do", "case", "lambda":
			return false
		case "module", "declarations", "class", "instance":
			return true
		}
		parent = parent.Parent()
	}
	return false
}

func haskellFunctionFromTree(node *tree_sitter.Node, source []byte, lines []string) haskellFunctionSpan {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return haskellFunctionSpan{}
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" || haskellIsKeyword(name) {
		return haskellFunctionSpan{}
	}
	startLine := shared.NodeLine(node)
	endLine := shared.NodeEndLine(node)
	return haskellFunctionSpan{
		name:      name,
		params:    haskellTreeFunctionParameters(node, source),
		source:    haskellLineRangeSource(lines, startLine, endLine),
		startLine: startLine,
		endLine:   endLine,
	}
}

func haskellTreeFunctionParameters(node *tree_sitter.Node, source []byte) map[string]struct{} {
	params := make(map[string]struct{})
	patterns := node.ChildByFieldName("patterns")
	if patterns == nil {
		return params
	}
	for _, child := range haskellNamedChildren(patterns) {
		child := child
		collectHaskellTreePatternParameters(&child, source, params)
	}
	return params
}

func collectHaskellTreePatternParameters(node *tree_sitter.Node, source []byte, params map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Kind() == "variable" || node.Kind() == "pat_name" {
		name := strings.TrimSpace(shared.NodeText(node, source))
		if haskellTreeParameterName(name) {
			params[name] = struct{}{}
			return
		}
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		collectHaskellTreePatternParameters(&child, source, params)
	}
}

func haskellTreeParameterName(name string) bool {
	if name == "" || haskellIsKeyword(name) || strings.ContainsAny(name, " \t\r\n()[]{}") {
		return false
	}
	return strings.ContainsAny(name[:1], "abcdefghijklmnopqrstuvwxyz_")
}

func applyHaskellTreeFunctionMetadata(
	payload map[string]any,
	syntax haskellSyntaxIndex,
	explicitExports map[string]struct{},
	isDependency bool,
	options shared.Options,
) {
	functions, _ := payload["functions"].([]map[string]any)
	for _, fn := range syntax.functions {
		item := haskellFunctionItem(functions, fn)
		created := false
		if item == nil {
			item = map[string]any{
				"name":          fn.name,
				"line_number":   fn.startLine,
				"end_line":      fn.endLine,
				"lang":          "haskell",
				"is_dependency": isDependency,
				"decorators":    []string{},
			}
			_, rootKinds := haskellFunctionContextAndRoots(fn.name, "", "", explicitExports)
			if len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			shared.AppendBucket(payload, "functions", item)
			functions, _ = payload["functions"].([]map[string]any)
			created = true
		}
		item["end_line"] = fn.endLine
		if options.IndexSource && (created || item["source"] == nil) {
			item["source"] = fn.source
		}
	}
}

func appendHaskellTreeFunctionCalls(
	payload map[string]any,
	syntax haskellSyntaxIndex,
	lines []string,
	seenCalls map[string]struct{},
) {
	functions, _ := payload["functions"].([]map[string]any)
	for _, fn := range syntax.functions {
		context := ""
		if item := haskellFunctionItem(functions, fn); item != nil {
			context, _ = item["class_context"].(string)
		}
		for lineNumber := fn.startLine; lineNumber <= fn.endLine && lineNumber <= len(lines); lineNumber++ {
			haskellAppendExpressionCalls(
				payload,
				lines[lineNumber-1],
				lineNumber,
				fn.name,
				context,
				fn.params,
				seenCalls,
			)
		}
	}
}

func haskellFunctionItem(functions []map[string]any, fn haskellFunctionSpan) map[string]any {
	for _, item := range functions {
		if item["name"] == fn.name && shared.IntValue(item["line_number"]) == fn.startLine {
			return item
		}
	}
	for _, item := range functions {
		if item["name"] == fn.name {
			return item
		}
	}
	return nil
}

func haskellNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func haskellLineRangeSource(lines []string, startLine int, endLine int) string {
	if startLine <= 0 || endLine < startLine || startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}
