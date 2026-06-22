package haskell

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// haskellSyntaxIndex holds the tree-sitter symbol view of a Haskell source file.
// It is the single source of truth for every primary payload bucket: the module
// header, classes-bucket declarations, class/instance methods, and top-level
// value bindings are all resolved from grammar nodes rather than a line scan.
type haskellSyntaxIndex struct {
	module      *haskellModuleSymbol
	types       []haskellTypeSymbol
	methods     []haskellMethodSymbol
	values      []haskellValueSymbol
	classBodies []haskellMethodSymbol
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
	index.collectSymbols(tree.RootNode(), source, lines)
	return source, index, nil
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
