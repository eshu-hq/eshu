package kotlin

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type kotlinSyntaxIndex struct {
	types      []kotlinTypeSpan
	functions  []kotlinFunctionSpan
	properties map[string]map[string]string
}

type kotlinTypeSpan struct {
	name      string
	startLine int
	endLine   int
}

type kotlinFunctionSpan struct {
	name         string
	classContext string
	startLine    int
	endLine      int
}

func kotlinSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, kotlinSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, kotlinSyntaxIndex{}, err
	}
	syntax, err := kotlinTreeSyntax(source, parser)
	if err != nil {
		return nil, kotlinSyntaxIndex{}, err
	}
	return source, syntax, nil
}

func kotlinTreeSyntax(source []byte, parser *tree_sitter.Parser) (kotlinSyntaxIndex, error) {
	if parser == nil {
		return kotlinSyntaxIndex{}, fmt.Errorf("parse kotlin tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return kotlinSyntaxIndex{}, fmt.Errorf("parse kotlin tree: parser returned nil tree")
	}
	defer tree.Close()

	index := kotlinSyntaxIndex{
		properties: make(map[string]map[string]string),
	}
	index.collect(tree.RootNode(), source, "")
	return index, nil
}

func (i *kotlinSyntaxIndex) collect(node *tree_sitter.Node, source []byte, currentType string) {
	if node == nil {
		return
	}
	nextType := currentType
	switch node.Kind() {
	case "class_declaration":
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			nextType = name
			i.types = append(i.types, kotlinTypeSpan{
				name:      name,
				startLine: shared.NodeLine(node),
				endLine:   shared.NodeEndLine(node),
			})
			i.collectClassParameters(name, node, source)
		}
	case "function_declaration":
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			i.functions = append(i.functions, kotlinFunctionSpan{
				name:         name,
				classContext: currentType,
				startLine:    shared.NodeLine(node),
				endLine:      shared.NodeEndLine(node),
			})
		}
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		i.collect(&child, source, nextType)
	}
}

func (i *kotlinSyntaxIndex) collectClassParameters(className string, node *tree_sitter.Node, source []byte) {
	if className == "" {
		return
	}
	primaryConstructor := kotlinPrimaryConstructor(node)
	if primaryConstructor == nil {
		return
	}
	shared.WalkNamed(primaryConstructor, func(child *tree_sitter.Node) {
		if child.Kind() != "class_parameter" {
			return
		}
		name := ""
		typ := ""
		cursor := child.Walk()
		defer cursor.Close()
		for _, parameterChild := range child.NamedChildren(cursor) {
			parameterChild := parameterChild
			switch parameterChild.Kind() {
			case "identifier":
				if name == "" {
					name = strings.TrimSpace(shared.NodeText(&parameterChild, source))
				}
			case "user_type":
				if typ == "" {
					typ = kotlinCanonicalTypeReference(shared.NodeText(&parameterChild, source))
				}
			}
		}
		if name == "" || typ == "" {
			return
		}
		if _, ok := i.properties[className]; !ok {
			i.properties[className] = make(map[string]string)
		}
		i.properties[className][name] = typ
	})
}

func kotlinPrimaryConstructor(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "primary_constructor" {
			return &child
		}
	}
	return nil
}

func (i kotlinSyntaxIndex) classPropertyTypes() map[string]map[string]string {
	values := make(map[string]map[string]string, len(i.properties))
	for className, properties := range i.properties {
		values[className] = make(map[string]string, len(properties))
		for name, typ := range properties {
			values[className][name] = typ
		}
	}
	return values
}

func (i kotlinSyntaxIndex) applyFunctionMetadata(item map[string]any, name string, line int) {
	treeFunction := i.functionAtStartLine(line)
	if treeFunction.name != name {
		return
	}
	item["end_line"] = treeFunction.endLine
	if _, ok := item["class_context"]; !ok && treeFunction.classContext != "" {
		item["class_context"] = treeFunction.classContext
	}
}

func (i kotlinSyntaxIndex) functionAtStartLine(line int) kotlinFunctionSpan {
	for _, fn := range i.functions {
		if fn.startLine == line {
			return fn
		}
	}
	return kotlinFunctionSpan{}
}

func (i kotlinSyntaxIndex) functionNameAtLineOr(current string, line int) string {
	if current != "" {
		return current
	}
	return i.functionNameAtLine(line)
}

func (i kotlinSyntaxIndex) functionNameAtLine(line int) string {
	for index := len(i.functions) - 1; index >= 0; index-- {
		fn := i.functions[index]
		if line >= fn.startLine && line <= fn.endLine {
			return fn.name
		}
	}
	return ""
}

func (i kotlinSyntaxIndex) typeNameAtLineOr(current string, line int) string {
	if current != "" {
		return current
	}
	return i.typeNameAtLine(line)
}

func (i kotlinSyntaxIndex) typeNameAtLine(line int) string {
	for index := len(i.types) - 1; index >= 0; index-- {
		typ := i.types[index]
		if line >= typ.startLine && line <= typ.endLine {
			return typ.name
		}
	}
	return ""
}
