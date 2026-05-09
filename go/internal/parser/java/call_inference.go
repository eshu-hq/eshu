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
	name     string
	typeName string
	line     int
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
			typeName := javaDeclaredTypeName(node, source)
			index.addReturn(classNode, name, typeName, nodeLine(node))
		case "formal_parameter":
			scope := javaCallInferenceScope(node)
			if scope == nil {
				return
			}
			name := javaParameterName(node, source)
			typeName := javaDeclaredTypeName(node, source)
			index.addVariable(scope, name, typeName, nodeLine(node))
		case "local_variable_declaration":
			scope := javaCallInferenceScope(node)
			if scope == nil {
				return
			}
			typeName := javaDeclaredTypeName(node, source)
			for _, name := range javaDeclarationVariableNames(node, source) {
				index.addVariable(scope, name, typeName, nodeLine(node))
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
			index.addVariable(scope, typedName.name, typedName.typeName, typedName.line)
		case "field_declaration":
			classNode := javaEnclosingClassNode(node)
			if classNode == nil {
				return
			}
			typeName := javaDeclaredTypeName(node, source)
			for _, name := range javaDeclarationVariableNames(node, source) {
				index.addField(classNode, name, typeName, nodeLine(node))
			}
		case "lambda_expression":
			scope := node
			for _, typedName := range javaLambdaTypedParameters(node, source) {
				index.addVariable(scope, typedName.name, typedName.typeName, typedName.line)
			}
		}
	})
	return index
}

func (i *javaCallInferenceIndex) addVariable(scope *tree_sitter.Node, name string, typeName string, line int) {
	if i == nil || scope == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(scope)
	i.variablesByScope[key] = append(i.variablesByScope[key], javaTypedName{
		name:     name,
		typeName: typeName,
		line:     line,
	})
}

func (i *javaCallInferenceIndex) addField(classNode *tree_sitter.Node, name string, typeName string, line int) {
	if i == nil || classNode == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(classNode)
	i.fieldsByClass[key] = append(i.fieldsByClass[key], javaTypedName{
		name:     name,
		typeName: typeName,
		line:     line,
	})
}

func (i *javaCallInferenceIndex) addReturn(classNode *tree_sitter.Node, name string, typeName string, line int) {
	if i == nil || classNode == nil || name == "" || typeName == "" {
		return
	}
	key := javaNodeRangeKey(classNode)
	i.returnsByClass[key] = append(i.returnsByClass[key], javaTypedName{
		name:     name,
		typeName: typeName,
		line:     line,
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
	if callNode == nil || callNode.Kind() != "method_invocation" {
		return ""
	}
	objectNode := callNode.ChildByFieldName("object")
	if objectNode == nil {
		return ""
	}
	if objectNode.Kind() == "object_creation_expression" {
		return javaObjectCreationTypeName(objectNode, source)
	}
	callLine := nodeLine(callNode)
	receiver := strings.TrimSpace(nodeText(objectNode, source))
	if fieldName := strings.TrimPrefix(receiver, "this."); fieldName != receiver && !strings.ContainsAny(fieldName, ".()[") {
		if index != nil {
			return index.fieldTypeBefore(javaEnclosingClassNode(callNode), fieldName, callLine+1)
		}
		return javaFieldTypeBefore(javaEnclosingClassNode(callNode), fieldName, source, callLine+1)
	}
	if className, fieldName, ok := javaExplicitOuterThisField(receiver); ok {
		if index != nil {
			return index.fieldTypeBefore(javaEnclosingClassNodeByName(callNode, source, className), fieldName, callLine+1)
		}
		return javaFieldTypeBefore(javaEnclosingClassNodeByName(callNode, source, className), fieldName, source, callLine+1)
	}
	if receiver == "" || strings.ContainsAny(receiver, ".()[") {
		return ""
	}
	if index != nil {
		if typeName := index.variableTypeBefore(javaCallInferenceScope(callNode), receiver, callLine); typeName != "" {
			return typeName
		}
		return index.fieldTypeBefore(javaEnclosingClassNode(callNode), receiver, callLine)
	}
	if typeName := javaVariableTypeBefore(javaCallInferenceScope(callNode), receiver, source, callLine); typeName != "" {
		return typeName
	}
	return javaFieldTypeBefore(javaEnclosingClassNode(callNode), receiver, source, callLine)
}

func (i *javaCallInferenceIndex) variableTypeBefore(
	scope *tree_sitter.Node,
	receiver string,
	beforeLine int,
) string {
	if i == nil || scope == nil || receiver == "" {
		return ""
	}
	for current := scope; current != nil; current = javaParentCallInferenceScope(current) {
		if typeName := javaTypeBefore(i.variablesByScope[javaNodeRangeKey(current)], receiver, beforeLine); typeName != "" {
			return typeName
		}
	}
	return ""
}

func (i *javaCallInferenceIndex) fieldTypeBefore(
	classNode *tree_sitter.Node,
	receiver string,
	beforeLine int,
) string {
	if i == nil || classNode == nil || receiver == "" {
		return ""
	}
	return javaTypeBefore(i.fieldsByClass[javaNodeRangeKey(classNode)], receiver, beforeLine)
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

func javaTypeBefore(entries []javaTypedName, receiver string, beforeLine int) string {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.name == receiver && entry.line < beforeLine {
			return entry.typeName
		}
	}
	return ""
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
	if scope == nil || receiver == "" {
		return ""
	}
	var matched string
	walkNamed(scope, func(node *tree_sitter.Node) {
		if matched != "" {
			return
		}
		switch node.Kind() {
		case "formal_parameter":
			if javaParameterName(node, source) == receiver {
				matched = javaDeclaredTypeName(node, source)
			}
		case "local_variable_declaration":
			if nodeLine(node) >= beforeLine {
				return
			}
			if javaDeclarationHasVariable(node, receiver, source) {
				matched = javaDeclaredTypeName(node, source)
			}
		}
	})
	return matched
}

func javaFieldTypeBefore(classNode *tree_sitter.Node, receiver string, source []byte, beforeLine int) string {
	if classNode == nil || receiver == "" {
		return ""
	}
	var matched string
	walkNamed(classNode, func(node *tree_sitter.Node) {
		if matched != "" || node.Kind() != "field_declaration" || nodeLine(node) >= beforeLine {
			return
		}
		if javaDeclarationHasVariable(node, receiver, source) {
			matched = javaDeclaredTypeName(node, source)
		}
	})
	return matched
}
