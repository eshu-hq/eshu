// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type dartTypeSpan struct {
	name      string
	extends   string
	startLine int
	endLine   int
}

type dartFunctionSpan struct {
	name         string
	classContext string
	decorators   []string
	source       string
	startLine    int
	endLine      int
	complexity   int
	isFactory    bool
}

type dartNamedSpan struct {
	name       string
	importType string
	line       int
}

type dartSyntaxIndex struct {
	types     []dartTypeSpan
	functions []dartFunctionSpan
	variables []dartNamedSpan
	imports   []dartNamedSpan
	calls     []dartCallSite
}

func dartSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, dartSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, dartSyntaxIndex{}, err
	}
	if parser == nil {
		return nil, dartSyntaxIndex{}, fmt.Errorf("parse dart tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, dartSyntaxIndex{}, fmt.Errorf("parse dart tree: parser returned nil tree")
	}
	defer tree.Close()

	index := dartSyntaxIndex{}
	lines := strings.Split(string(source), "\n")
	index.collect(tree.RootNode(), source, lines, dartTypeSpan{})
	index.calls = collectDartCallSites(tree.RootNode(), source)
	return source, index, nil
}

func (i *dartSyntaxIndex) collect(node *tree_sitter.Node, source []byte, lines []string, scope dartTypeSpan) {
	if node == nil {
		return
	}

	nextScope := scope
	switch node.Kind() {
	case "library_import":
		if imported := dartQuotedURI(shared.NodeText(node, source)); imported != "" {
			i.imports = append(i.imports, dartNamedSpan{
				name:       imported,
				importType: "import",
				line:       shared.NodeLine(node),
			})
		}
	case "library_export":
		if imported := dartQuotedURI(shared.NodeText(node, source)); imported != "" {
			i.imports = append(i.imports, dartNamedSpan{
				name:       imported,
				importType: "export",
				line:       shared.NodeLine(node),
			})
		}
	case "class_definition", "mixin_declaration", "enum_declaration", "extension_declaration", "extension_type_declaration":
		if typ := dartTypeFromNode(node, source); typ.name != "" {
			nextScope = typ
			i.types = append(i.types, typ)
		}
	case "method_signature":
		if fn := dartFunctionFromNode(node, source, lines, scope); fn.name != "" {
			i.functions = append(i.functions, fn)
		}
		return
	case "constructor_signature", "constant_constructor_signature", "factory_constructor_signature", "redirecting_factory_constructor_signature":
		if dartParentKind(node) != "method_signature" {
			if fn := dartFunctionFromNode(node, source, lines, scope); fn.name != "" {
				i.functions = append(i.functions, fn)
			}
			return
		}
	case "function_signature":
		if dartParentKind(node) != "method_signature" {
			if fn := dartFunctionFromNode(node, source, lines, scope); fn.name != "" {
				i.functions = append(i.functions, fn)
			}
			return
		}
	case "initialized_identifier", "initialized_variable_definition", "static_final_declaration":
		if name := dartVariableName(node, source); name != "" {
			i.variables = append(i.variables, dartNamedSpan{name: name, line: shared.NodeLine(node)})
		}
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		i.collect(&child, source, lines, nextScope)
	}
}

func dartTypeFromNode(node *tree_sitter.Node, source []byte) dartTypeSpan {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		name = dartFirstIdentifier(node, source)
	}
	return dartTypeSpan{
		name:      name,
		extends:   dartSuperclass(node.ChildByFieldName("superclass"), source),
		startLine: shared.NodeLine(node),
		endLine:   shared.NodeEndLine(node),
	}
}

func dartFunctionFromNode(node *tree_sitter.Node, source []byte, lines []string, scope dartTypeSpan) dartFunctionSpan {
	name := dartCallableName(node, source)
	if name == "" {
		return dartFunctionSpan{}
	}
	startLine := shared.NodeLine(node)
	endNode := dartDeclarationEndNode(node)
	bodyNode := endNode
	if bodyNode != nil && bodyNode.Kind() != "function_body" {
		bodyNode = nil
	}
	isFactory := isDartFactoryConstructor(node)
	return dartFunctionSpan{
		name:         name,
		classContext: scope.name,
		decorators:   dartDecoratorsBeforeLine(lines, startLine),
		source:       strings.TrimSpace(dartNodeRangeText(node, endNode, source)),
		startLine:    startLine,
		endLine:      dartFunctionEndLine(endNode, lines),
		complexity:   dartCyclomaticComplexity(node, bodyNode, source),
		isFactory:    isFactory,
	}
}

func dartCallableName(node *tree_sitter.Node, source []byte) string {
	switch node.Kind() {
	case "constructor_signature", "constant_constructor_signature", "factory_constructor_signature", "redirecting_factory_constructor_signature":
		if name := dartNameBeforeParameters(shared.NodeText(node, source)); name != "" {
			return name
		}
	}
	if name := dartNodeName(node, source); name != "" {
		return name
	}
	for _, child := range dartNamedChildren(node) {
		child := child
		switch child.Kind() {
		case "constructor_signature", "constant_constructor_signature", "factory_constructor_signature", "redirecting_factory_constructor_signature", "function_signature", "getter_signature", "setter_signature":
			if name := dartCallableName(&child, source); name != "" {
				return name
			}
		}
	}
	return dartNameBeforeParameters(shared.NodeText(node, source))
}

func dartNodeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	return ""
}

func dartVariableName(node *tree_sitter.Node, source []byte) string {
	if name := dartNodeName(node, source); name != "" {
		return name
	}
	return dartFirstIdentifier(node, source)
}

func dartFirstIdentifier(node *tree_sitter.Node, source []byte) string {
	for _, child := range dartNamedChildren(node) {
		child := child
		if child.Kind() == "identifier" {
			return strings.TrimSpace(shared.NodeText(&child, source))
		}
	}
	return ""
}

func dartSuperclass(node *tree_sitter.Node, source []byte) string {
	text := strings.TrimSpace(shared.NodeText(node, source))
	text = strings.TrimSpace(strings.TrimPrefix(text, "extends"))
	if text == "" {
		return ""
	}
	fields := strings.FieldsFunc(text, func(character rune) bool {
		return character == '{' || character == ',' || unicode.IsSpace(character)
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

func dartDeclarationEndNode(node *tree_sitter.Node) *tree_sitter.Node {
	for sibling := node.NextNamedSibling(); sibling != nil; sibling = sibling.NextNamedSibling() {
		switch sibling.Kind() {
		case "function_body":
			return sibling
		case "method_signature", "function_signature", "class_definition", "mixin_declaration", "enum_declaration", "extension_declaration":
			return node
		}
	}
	if parent := node.Parent(); parent != nil {
		for sibling := parent.NextNamedSibling(); sibling != nil; sibling = sibling.NextNamedSibling() {
			if sibling.Kind() == "function_body" {
				return sibling
			}
			if sibling.Kind() == "method_signature" || sibling.Kind() == "function_signature" {
				return node
			}
		}
	}
	return node
}

func dartFunctionEndLine(node *tree_sitter.Node, lines []string) int {
	endLine := shared.NodeEndLine(node)
	if node == nil || node.Kind() != "function_body" || endLine <= 1 || endLine > len(lines) {
		return endLine
	}
	if strings.TrimSpace(lines[endLine-1]) == "}" {
		return endLine - 1
	}
	return endLine
}

func dartNodeRangeText(start *tree_sitter.Node, end *tree_sitter.Node, source []byte) string {
	if start == nil || end == nil || end.EndByte() < start.StartByte() {
		return shared.NodeText(start, source)
	}
	return string(source[start.StartByte():end.EndByte()])
}

func dartNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func dartParentKind(node *tree_sitter.Node) string {
	if node == nil || node.Parent() == nil {
		return ""
	}
	return node.Parent().Kind()
}

func dartNameBeforeParameters(text string) string {
	index := strings.Index(text, "(")
	if index < 0 {
		return ""
	}
	prefix := strings.TrimSpace(text[:index])
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return ""
	}
	name := fields[len(fields)-1]
	name = strings.TrimPrefix(name, "const ")
	name = strings.TrimPrefix(name, "factory ")
	return strings.TrimSpace(name)
}

func dartQuotedURI(text string) string {
	for index, character := range text {
		if character != '\'' && character != '"' {
			continue
		}
		quote := character
		start := index + utf8.RuneLen(character)
		for offset, next := range text[start:] {
			if next == quote {
				return text[start : start+offset]
			}
		}
		return ""
	}
	return ""
}

func isDartFactoryConstructor(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind() == "factory_constructor_signature" || node.Kind() == "redirecting_factory_constructor_signature" {
		return true
	}
	for _, child := range dartNamedChildren(node) {
		if child.Kind() == "factory_constructor_signature" || child.Kind() == "redirecting_factory_constructor_signature" {
			return true
		}
	}
	return false
}

func dartDecoratorsBeforeLine(lines []string, line int) []string {
	var decorators []string
	for index := line - 2; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "@") {
			break
		}
		decorators = append(decorators, strings.Fields(trimmed)[0])
	}
	slices.Reverse(decorators)
	return decorators
}
