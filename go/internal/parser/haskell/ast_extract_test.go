// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package haskell

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseCapturesMultiLineTypeSignatureClassMethod proves the AST path records
// a typeclass method whose name and `::` sit on separate lines. The former
// line-scan `haskellTypeSignaturePattern` (`^\s*([a-z_][A-Za-z0-9_']*)\s*::`)
// required the `::` token on the same trimmed line as the name, so a wrapped
// signature where the name stands alone produced no method row. The AST keys the
// method on the `signature` node, so the wrap no longer hides it. This is the
// failing-first deviation: it fails against the regex line-scan and passes on the
// AST path.
func TestParseCapturesMultiLineTypeSignatureClassMethod(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "MultiSig.hs", `module MultiSig where

class Service a where
  perform
    :: a
    -> IO ()
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	method := assertBucketField(t, payload, "functions", "class_context", "Service")
	if got := method["name"]; got != "perform" {
		t.Fatalf("class_context Service function name = %#v, want perform", got)
	}
	assertParserStringSliceContains(t, method, "dead_code_root_kinds", "haskell.typeclass_method")
}

// TestParseCapturesTypeExportInModuleHeader proves the AST export parse marks a
// data declaration that the module exports as `Type(..)` as an exported-type
// dead-code root. The export list is read from the header AST node rather than a
// bounded substring of the header text.
func TestParseCapturesTypeExportInModuleHeader(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Exported.hs", `module Exported
  ( Widget(..)
  , render
  ) where

data Widget = Widget Int

render widget = widget
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	widget := assertBucketName(t, payload, "classes", "Widget")
	assertParserStringSliceContains(t, widget, "dead_code_root_kinds", "haskell.exported_type")
	render := assertBucketName(t, payload, "functions", "render")
	assertParserStringSliceContains(t, render, "dead_code_root_kinds", "haskell.module_export")
}

// TestParseCapturesDataNewtypeTypeKinds proves the AST path emits each type-level
// declaration kind into the classes bucket with its semantic_kind.
func TestParseCapturesDataNewtypeTypeKinds(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Kinds.hs", `module Kinds where

data Color = Red | Blue
newtype Wrapper = Wrapper Int
type Alias = Int
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for name, kind := range map[string]string{"Color": "data", "Wrapper": "newtype", "Alias": "type"} {
		item := assertBucketName(t, payload, "classes", name)
		if got := item["semantic_kind"]; got != kind {
			t.Fatalf("classes[%s][semantic_kind] = %#v, want %q", name, got, kind)
		}
	}
}

// TestHaskellTreeFunctionParametersReadPatternWrappers confirms the AST parameter
// collector descends constructor pattern wrappers such as `(Just value)` to the
// inner bound variable. This coverage moved here from the removed
// tree_sitter_syntax_test.go when parameter extraction folded into the AST nodes.
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

	if fn := firstHaskellTreeFunctionNamedOrNil(node, source, name); fn != nil {
		return fn
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
