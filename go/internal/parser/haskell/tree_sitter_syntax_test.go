package haskell

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
)

func TestHaskellTreeFunctionParametersReadPatternWrappers(t *testing.T) {
	t.Parallel()

	source := []byte(`module ParameterCalls where

caller (Just value) = helper value
`)
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_haskell.Language())
	if err := parser.SetLanguage(language); err != nil {
		t.Fatalf("SetLanguage(haskell) error = %v, want nil", err)
	}
	defer parser.Close()

	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatalf("Parse() returned nil tree")
	}
	defer tree.Close()

	fn := firstHaskellTreeFunctionNamed(t, tree.RootNode(), source, "caller")
	params := haskellTreeFunctionParameters(fn, source)
	if _, ok := params["value"]; !ok {
		t.Fatalf("haskellTreeFunctionParameters(caller) missing value in %#v", params)
	}
}

func firstHaskellTreeFunctionNamed(t *testing.T, node *tree_sitter.Node, source []byte, name string) *tree_sitter.Node {
	t.Helper()

	if node == nil {
		t.Fatalf("syntax tree missing function %q", name)
	}
	if node.Kind() == "function" {
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil && string(nameNode.Utf8Text(source)) == name {
			return node
		}
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		if fn := firstHaskellTreeFunctionNamedOrNil(&child, source, name); fn != nil {
			return fn
		}
	}
	t.Fatalf("syntax tree missing function %q", name)
	return nil
}

func firstHaskellTreeFunctionNamedOrNil(node *tree_sitter.Node, source []byte, name string) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "function" {
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil && string(nameNode.Utf8Text(source)) == name {
			return node
		}
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		if fn := firstHaskellTreeFunctionNamedOrNil(&child, source, name); fn != nil {
			return fn
		}
	}
	return nil
}
