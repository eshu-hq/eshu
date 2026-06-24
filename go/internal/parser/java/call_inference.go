// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaNodeKey struct {
	start uint
	end   uint
	kind  string
}

type javaTypedName struct {
	name              string
	typeName          string
	qualifiedTypeName string
	line              int
}

type javaCallInferenceIndex struct {
	variablesByScope map[javaNodeKey][]javaTypedName
	fieldsByClass    map[javaNodeKey][]javaTypedName
	returnsByClass   map[javaNodeKey][]javaTypedName
}

func buildJavaCallInferenceIndex(root *tree_sitter.Node, source []byte) *javaCallInferenceIndex {
	index := &javaCallInferenceIndex{
		variablesByScope: map[javaNodeKey][]javaTypedName{},
		fieldsByClass:    map[javaNodeKey][]javaTypedName{},
		returnsByClass:   map[javaNodeKey][]javaTypedName{},
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "method_declaration":
			classNode := javaEnclosingClassNode(node)
			if classNode == nil {
				return
			}
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			typeName, qualifiedTypeName := javaDeclaredTypeNames(node, source)
			index.addReturn(classNode, name, typeName, qualifiedTypeName, nodeLine(node))
		case "formal_parameter":
			scope := javaCallInferenceScope(node)
			if scope == nil {
				return
			}
			name := javaParameterName(node, source)
			typeName, qualifiedTypeName := javaDeclaredTypeNames(node, source)
			index.addVariable(scope, name, typeName, qualifiedTypeName, nodeLine(node))
		case "local_variable_declaration":
			scope := javaCallInferenceScope(node)
			if scope == nil {
				return
			}
			typeName, qualifiedTypeName := javaDeclaredTypeNames(node, source)
			for _, name := range javaDeclarationVariableNames(node, source) {
				index.addVariable(scope, name, typeName, qualifiedTypeName, nodeLine(node))
			}
		case "enhanced_for_statement":
			scope := javaCallInferenceScope(node)
			if scope == nil {
				return
			}
			typedName, ok := javaEnhancedForTypedName(node, source)
			if !ok {
				return
			}
			index.addVariable(scope, typedName.name, typedName.typeName, typedName.qualifiedTypeName, typedName.line)
		case "field_declaration":
			classNode := javaEnclosingClassNode(node)
			if classNode == nil {
				return
			}
			typeName, qualifiedTypeName := javaDeclaredTypeNames(node, source)
			for _, name := range javaDeclarationVariableNames(node, source) {
				index.addField(classNode, name, typeName, qualifiedTypeName, nodeLine(node))
			}
		case "lambda_expression":
			scope := node
			for _, typedName := range javaLambdaTypedParameters(node, source) {
				index.addVariable(scope, typedName.name, typedName.typeName, typedName.qualifiedTypeName, typedName.line)
			}
		}
	})
	return index
}

func (i *javaCallInferenceIndex) addVariable(
	scope *tree_sitter.Node,
	name string,
	typeName string,
	qualifiedTypeName string,
	line int,
) {
	if i == nil || scope == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(scope)
	i.variablesByScope[key] = append(i.variablesByScope[key], javaTypedName{
		name:              name,
		typeName:          typeName,
		qualifiedTypeName: qualifiedTypeName,
		line:              line,
	})
}

func (i *javaCallInferenceIndex) addField(
	classNode *tree_sitter.Node,
	name string,
	typeName string,
	qualifiedTypeName string,
	line int,
) {
	if i == nil || classNode == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(classNode)
	i.fieldsByClass[key] = append(i.fieldsByClass[key], javaTypedName{
		name:              name,
		typeName:          typeName,
		qualifiedTypeName: qualifiedTypeName,
		line:              line,
	})
}

func (i *javaCallInferenceIndex) addReturn(
	classNode *tree_sitter.Node,
	name string,
	typeName string,
	qualifiedTypeName string,
	line int,
) {
	if i == nil || classNode == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(classNode)
	i.returnsByClass[key] = append(i.returnsByClass[key], javaTypedName{
		name:              name,
		typeName:          typeName,
		qualifiedTypeName: qualifiedTypeName,
		line:              line,
	})
}

func javaNodeRangeKey(node *tree_sitter.Node) javaNodeKey {
	if node == nil {
		return javaNodeKey{}
	}
	return javaNodeKey{
		start: node.StartByte(),
		end:   node.EndByte(),
		kind:  node.Kind(),
	}
}

// javaCallInferredObjectType attaches bounded receiver type evidence for Java
// method calls when the receiver is a local variable, parameter, field, or
// inline constructor expression visible in the parsed source.
func javaCallInferredObjectType(
	callNode *tree_sitter.Node,
	source []byte,
	index *javaCallInferenceIndex,
) string {
	typeName, _ := javaCallInferredObjectTypes(callNode, source, index)
	return typeName
}

func javaCallInferredObjectQualifiedType(
	callNode *tree_sitter.Node,
	source []byte,
	index *javaCallInferenceIndex,
) string {
	_, qualifiedTypeName := javaCallInferredObjectTypes(callNode, source, index)
	return qualifiedTypeName
}

func javaCallInferredObjectTypes(
	callNode *tree_sitter.Node,
	source []byte,
	index *javaCallInferenceIndex,
) (string, string) {
	if callNode == nil || callNode.Kind() != "method_invocation" {
		return "", ""
	}
	objectNode := callNode.ChildByFieldName("object")
	if objectNode == nil {
		return "", ""
	}
	if objectNode.Kind() == "object_creation_expression" {
		return javaObjectCreationTypeNames(objectNode, source)
	}
	callLine := nodeLine(callNode)
	receiver := strings.TrimSpace(nodeText(objectNode, source))
	if fieldName := strings.TrimPrefix(receiver, "this."); fieldName != receiver && !strings.ContainsAny(fieldName, ".()[") {
		if index != nil {
			return index.fieldTypesBefore(javaEnclosingClassNode(callNode), fieldName, callLine+1)
		}
		return javaFieldTypesBefore(javaEnclosingClassNode(callNode), fieldName, source, callLine+1)
	}
	if className, fieldName, ok := javaExplicitOuterThisField(receiver); ok {
		if index != nil {
			return index.fieldTypesBefore(javaEnclosingClassNodeByName(callNode, source, className), fieldName, callLine+1)
		}
		return javaFieldTypesBefore(javaEnclosingClassNodeByName(callNode, source, className), fieldName, source, callLine+1)
	}
	if receiver == "" || strings.ContainsAny(receiver, ".()[") {
		return "", ""
	}
	if index != nil {
		if typeName, qualifiedTypeName := index.variableTypesBefore(javaCallInferenceScope(callNode), receiver, callLine); typeName != "" {
			return typeName, qualifiedTypeName
		}
		return index.fieldTypesBefore(javaEnclosingClassNode(callNode), receiver, callLine)
	}
	if typeName, qualifiedTypeName := javaVariableTypesBefore(javaCallInferenceScope(callNode), receiver, source, callLine); typeName != "" {
		return typeName, qualifiedTypeName
	}
	return javaFieldTypesBefore(javaEnclosingClassNode(callNode), receiver, source, callLine)
}

func (i *javaCallInferenceIndex) variableTypeBefore(
	scope *tree_sitter.Node,
	receiver string,
	beforeLine int,
) string {
	typeName, _ := i.variableTypesBefore(scope, receiver, beforeLine)
	return typeName
}

func (i *javaCallInferenceIndex) variableTypesBefore(
	scope *tree_sitter.Node,
	receiver string,
	beforeLine int,
) (string, string) {
	if i == nil || scope == nil || receiver == "" {
		return "", ""
	}
	for current := scope; current != nil; current = javaParentCallInferenceScope(current) {
		if typedName, ok := javaTypedNameBefore(i.variablesByScope[javaNodeRangeKey(current)], receiver, beforeLine); ok {
			return typedName.typeName, typedName.qualifiedTypeName
		}
	}
	return "", ""
}

func (i *javaCallInferenceIndex) fieldTypeBefore(
	classNode *tree_sitter.Node,
	receiver string,
	beforeLine int,
) string {
	typeName, _ := i.fieldTypesBefore(classNode, receiver, beforeLine)
	return typeName
}

func (i *javaCallInferenceIndex) fieldTypesBefore(
	classNode *tree_sitter.Node,
	receiver string,
	beforeLine int,
) (string, string) {
	if i == nil || classNode == nil || receiver == "" {
		return "", ""
	}
	if typedName, ok := javaTypedNameBefore(i.fieldsByClass[javaNodeRangeKey(classNode)], receiver, beforeLine); ok {
		return typedName.typeName, typedName.qualifiedTypeName
	}
	return "", ""
}

func (i *javaCallInferenceIndex) methodReturnType(
	classNode *tree_sitter.Node,
	name string,
) string {
	if i == nil || classNode == nil || name == "" {
		return ""
	}
	entries := i.returnsByClass[javaNodeRangeKey(classNode)]
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.name == name {
			return entry.typeName
		}
	}
	return ""
}

func javaTypedNameBefore(entries []javaTypedName, receiver string, beforeLine int) (javaTypedName, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.name == receiver && entry.line < beforeLine {
			return entry, true
		}
	}
	return javaTypedName{}, false
}

func javaCallInferenceScope(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "method_declaration", "constructor_declaration", "lambda_expression":
			return current
		}
	}
	return nil
}

func javaParentCallInferenceScope(scope *tree_sitter.Node) *tree_sitter.Node {
	if scope == nil {
		return nil
	}
	for current := scope.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "method_declaration", "constructor_declaration", "lambda_expression":
			return current
		}
	}
	return nil
}

func javaEnclosingClassNode(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
			return current
		}
	}
	return nil
}

func javaEnclosingClassNodeByName(node *tree_sitter.Node, source []byte, name string) *tree_sitter.Node {
	if node == nil || name == "" {
		return nil
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
			if strings.TrimSpace(nodeText(current.ChildByFieldName("name"), source)) == name {
				return current
			}
		}
	}
	return nil
}

func javaVariableTypeBefore(scope *tree_sitter.Node, receiver string, source []byte, beforeLine int) string {
	typeName, _ := javaVariableTypesBefore(scope, receiver, source, beforeLine)
	return typeName
}

func javaVariableTypesBefore(scope *tree_sitter.Node, receiver string, source []byte, beforeLine int) (string, string) {
	if scope == nil || receiver == "" {
		return "", ""
	}
	var matchedType string
	var matchedQualifiedType string
	walkNamed(scope, func(node *tree_sitter.Node) {
		if matchedType != "" {
			return
		}
		switch node.Kind() {
		case "formal_parameter":
			if javaParameterName(node, source) == receiver {
				matchedType, matchedQualifiedType = javaDeclaredTypeNames(node, source)
			}
		case "local_variable_declaration":
			if nodeLine(node) >= beforeLine {
				return
			}
			if javaDeclarationHasVariable(node, receiver, source) {
				matchedType, matchedQualifiedType = javaDeclaredTypeNames(node, source)
			}
		}
	})
	return matchedType, matchedQualifiedType
}

func javaFieldTypeBefore(classNode *tree_sitter.Node, receiver string, source []byte, beforeLine int) string {
	typeName, _ := javaFieldTypesBefore(classNode, receiver, source, beforeLine)
	return typeName
}

func javaFieldTypesBefore(classNode *tree_sitter.Node, receiver string, source []byte, beforeLine int) (string, string) {
	if classNode == nil || receiver == "" {
		return "", ""
	}
	var matchedType string
	var matchedQualifiedType string
	walkNamed(classNode, func(node *tree_sitter.Node) {
		if matchedType != "" || node.Kind() != "field_declaration" || nodeLine(node) >= beforeLine {
			return
		}
		if javaDeclarationHasVariable(node, receiver, source) {
			matchedType, matchedQualifiedType = javaDeclaredTypeNames(node, source)
		}
	})
	return matchedType, matchedQualifiedType
}
