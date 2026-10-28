// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// ExtractSetupPYRuntimeEvidence is the content-only entrypoint for setup.py.
// It uses the same AST reader as the native Python parser and executes no code.
func ExtractSetupPYRuntimeEvidence(source []byte) (pythondep.PythonRuntimeEvidence, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		return pythondep.PythonRuntimeEvidence{State: "unavailable", ByteSize: len(source)}, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return pythondep.PythonRuntimeEvidence{State: "unavailable", ByteSize: len(source)}, fmt.Errorf("parse setup.py: nil syntax tree")
	}
	defer tree.Close()
	return pythonSetupRuntimeEvidence(tree.RootNode(), source), nil
}

func pythonSetupRuntimeEvidence(root *tree_sitter.Node, source []byte) pythondep.PythonRuntimeEvidence {
	evidence := pythondep.PythonRuntimeEvidence{State: "parsed_empty", ByteSize: len(source), Digest: pythondep.PythonRuntimeDigest(source)}
	if len(source) > 1<<20 {
		evidence.State = "over_cap"
		return evidence
	}
	if root == nil || root.HasError() {
		evidence.State = "unavailable"
		return evidence
	}
	direct := make(map[string]bool)
	modules := make(map[string]bool)
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "import_statement":
			bindSetupModuleImport(&child, source, modules, direct)
		case "import_from_statement":
			bindSetupDirectImport(&child, source, modules, direct)
		case "expression_statement":
			expression := pythonFirstNamedChild(&child)
			if expression == nil {
				continue
			}
			switch expression.Kind() {
			case "assignment", "augmented_assignment", "annotated_assignment":
				left := expression.ChildByFieldName("left")
				if left != nil && left.Kind() == "identifier" {
					delete(direct, nodeText(left, source))
					delete(modules, nodeText(left, source))
				}
			case "call":
				if !boundSetupCall(expression, source, modules, direct) {
					continue
				}
				value, line, found, dynamic := setupPythonRequiresKeyword(expression, source)
				if !found {
					continue
				}
				evidence.TokenCount++
				if evidence.TokenCount > shared.RuntimeVersionTokenCap {
					continue
				}
				if dynamic || !shared.SafeVersionSpec(value) {
					evidence.Rejected++
					continue
				}
				evidence.Values = append(evidence.Values, pythondep.PythonRuntimeValue{
					SourceKind: "setup_py_python_requires", Value: strings.Clone(value), Line: line,
				})
			}
		case "function_definition", "class_definition":
			name := nodeText(child.ChildByFieldName("name"), source)
			delete(direct, name)
			delete(modules, name)
		}
	}
	if evidence.TokenCount > shared.RuntimeVersionTokenCap {
		evidence.Values = nil
		evidence.Rejected = 0
		evidence.State = "over_tokens"
		return evidence
	}
	if len(evidence.Values) > 0 || evidence.Rejected > 0 {
		evidence.State = "parsed_values"
	}
	return evidence
}

func bindSetupModuleImport(node *tree_sitter.Node, source []byte, modules, direct map[string]bool) {
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		child := node.Child(i)
		nameNode := child
		alias := ""
		if child.Kind() == "aliased_import" {
			nameNode = child.ChildByFieldName("name")
			alias = nodeText(child.ChildByFieldName("alias"), source)
		}
		module := nodeText(nameNode, source)
		if alias == "" {
			alias = strings.Split(module, ".")[0]
		}
		delete(direct, alias)
		modules[alias] = module == "setuptools" || (module == "distutils.core" && alias != "distutils")
	}
}

func bindSetupDirectImport(node *tree_sitter.Node, source []byte, modules, direct map[string]bool) {
	module := nodeText(node.ChildByFieldName("module_name"), source)
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		child := node.Child(i)
		nameNode := child
		alias := ""
		if child.Kind() == "aliased_import" {
			nameNode = child.ChildByFieldName("name")
			alias = nodeText(child.ChildByFieldName("alias"), source)
		}
		name := nodeText(nameNode, source)
		if alias == "" {
			alias = name
		}
		delete(modules, alias)
		direct[alias] = name == "setup" && (module == "setuptools" || module == "distutils.core")
	}
}

func boundSetupCall(call *tree_sitter.Node, source []byte, modules, direct map[string]bool) bool {
	function := call.ChildByFieldName("function")
	if function == nil {
		return false
	}
	if function.Kind() == "identifier" {
		return direct[nodeText(function, source)]
	}
	if function.Kind() != "attribute" || nodeText(function.ChildByFieldName("attribute"), source) != "setup" {
		return false
	}
	object := function.ChildByFieldName("object")
	return object != nil && object.Kind() == "identifier" && modules[nodeText(object, source)]
}

func setupPythonRequiresKeyword(call *tree_sitter.Node, source []byte) (string, int, bool, bool) {
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil {
		return "", 0, false, false
	}
	var value string
	var line int
	found, dynamic := false, false
	cursor := arguments.Walk()
	defer cursor.Close()
	for _, child := range arguments.NamedChildren(cursor) {
		child := child
		if child.Kind() == "dictionary_splat" {
			dynamic = true
		}
		if child.Kind() != "keyword_argument" || nodeText(child.ChildByFieldName("name"), source) != "python_requires" {
			continue
		}
		if found {
			dynamic = true
		}
		found = true
		literal := child.ChildByFieldName("value")
		if literal == nil || literal.Kind() != "string" {
			dynamic = true
			continue
		}
		raw := nodeText(literal, source)
		if raw == "" || (raw[0] != '\'' && raw[0] != '"') || strings.Contains(raw, "\\") {
			dynamic = true
			continue
		}
		value = pythonStringLiteralValue(literal, source)
		line = nodeLine(&child)
	}
	return value, line, found, dynamic
}
