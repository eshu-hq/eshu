package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectPHPClassDeclaration emits a class row, records inheritance and trait
// evidence, captures property types, and walks owned methods.
func collectPHPClassDeclaration(state *phpParseState, node *tree_sitter.Node) {
	name := phpDeclarationName(node, state.source)
	if name == "" {
		return
	}
	bases := phpClassBases(node, state.source)
	item := phpTypeItem(name, node)
	if len(bases) > 0 {
		item["bases"] = bases
	}
	recordPHPDeadCodeType(state.deadCodeFacts, "class_declaration", name, bases)
	if parent := phpClassExtendsBase(node, state.source); parent != "" {
		state.classParentTypes[name] = normalizePHPImportedTypeName(parent, state.importAliases)
	}
	if adaptations := phpClassTraitAdaptations(node, state.source); len(adaptations) > 0 {
		item["trait_adaptations"] = adaptations
	}
	shared.AppendBucket(state.payload, "classes", item)
	collectPHPTypeMembers(state, node, name, "class_declaration")
}

// collectPHPInterfaceDeclaration emits an interface row, records inheritance,
// and walks declared methods.
func collectPHPInterfaceDeclaration(state *phpParseState, node *tree_sitter.Node) {
	name := phpDeclarationName(node, state.source)
	if name == "" {
		return
	}
	bases := phpInterfaceBases(node, state.source)
	item := phpTypeItem(name, node)
	if len(bases) > 0 {
		item["bases"] = bases
	}
	recordPHPDeadCodeType(state.deadCodeFacts, "interface_declaration", name, bases)
	shared.AppendBucket(state.payload, "interfaces", item)
	collectPHPTypeMembers(state, node, name, "interface_declaration")
}

// collectPHPTraitDeclaration emits a trait row and walks declared methods.
func collectPHPTraitDeclaration(state *phpParseState, node *tree_sitter.Node) {
	name := phpDeclarationName(node, state.source)
	if name == "" {
		return
	}
	item := phpTypeItem(name, node)
	recordPHPDeadCodeType(state.deadCodeFacts, "trait_declaration", name, nil)
	shared.AppendBucket(state.payload, "traits", item)
	collectPHPTypeMembers(state, node, name, "trait_declaration")
}

// collectPHPAnonymousClass emits a synthetic class row for a new-class
// expression and records its parent type for receiver inference.
func collectPHPAnonymousClass(state *phpParseState, node *tree_sitter.Node) {
	name := phpAnonymousClassName(shared.NodeLine(node))
	bases := phpClassBases(node, state.source)
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeLine(node),
		"lang":        "php",
	}
	if len(bases) > 0 {
		item["bases"] = bases
	}
	if parent := phpClassExtendsBase(node, state.source); parent != "" {
		state.classParentTypes[name] = normalizePHPImportedTypeName(parent, state.importAliases)
	}
	shared.AppendBucket(state.payload, "classes", item)
	collectPHPTypeMembers(state, node, name, "class_declaration")
}

// collectPHPTypeMembers records property types and walks method declarations
// owned directly by a class, interface, or trait declaration list.
func collectPHPTypeMembers(state *phpParseState, typeNode *tree_sitter.Node, typeName string, typeKind string) {
	list := phpDeclarationList(typeNode)
	if list == nil {
		return
	}
	cursor := list.Walk()
	defer cursor.Close()
	for _, member := range list.NamedChildren(cursor) {
		member := member
		switch member.Kind() {
		case "property_declaration":
			collectPHPPropertyTypes(state, &member, typeName)
		case "method_declaration":
			collectPHPFunction(state, &member, typeName, typeKind)
		}
	}
}

// collectPHPPropertyTypes records the declared type for each property element so
// later receiver chains can resolve `$this->prop` and `Class::$prop`.
func collectPHPPropertyTypes(state *phpParseState, node *tree_sitter.Node, typeName string) {
	declaredType := phpTypeNodeName(phpMemberTypeNode(node), state.source)
	if declaredType == "" {
		return
	}
	normalized := normalizePHPImportedTypeName(declaredType, state.importAliases)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "property_element" {
			continue
		}
		variable := phpPropertyElementName(&child, state.source)
		if variable == "" {
			continue
		}
		if state.classPropertyTypes[typeName] == nil {
			state.classPropertyTypes[typeName] = make(map[string]string)
		}
		state.classPropertyTypes[typeName][variable] = normalized
	}
}

// collectPHPFunction emits a function or method row, records parameter and
// return-type evidence, and stages the row for dead-code root classification.
func collectPHPFunction(state *phpParseState, node *tree_sitter.Node, typeName string, typeKind string) {
	name := phpDeclarationName(node, state.source)
	if name == "" {
		return
	}
	parameters := phpFunctionParameterNames(node, state.source)
	returnType := phpFunctionReturnType(node, state.source)

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(phpNameNode(node)),
		"end_line":    shared.NodeLine(phpNameNode(node)),
		"lang":        "php",
		"decorators":  []string{},
		"parameters":  parameters,
	}
	if typeName != "" {
		item["class_context"] = typeName
		if semanticKind := phpSemanticKindForMethod(name); semanticKind != "" {
			item["semantic_kind"] = semanticKind
		}
		if returnType != "" {
			if state.methodReturnTypes[typeName] == nil {
				state.methodReturnTypes[typeName] = make(map[string]string)
			}
			state.methodReturnTypes[typeName][name] = returnType
		}
	} else if returnType != "" {
		state.functionReturnTypes[name] = returnType
	}
	if returnType != "" {
		item["return_type"] = returnType
	}
	if state.indexSource {
		item["source"] = shared.NodeText(node, state.source)
	}

	state.recordPHPFunctionParameterTypes(node, typeName, name)
	recordPHPDeadCodeFunction(state.deadCodeFacts, name, typeName, typeKind, parameters)
	state.deadCodeFunctions = append(state.deadCodeFunctions, phpDeadCodeFunctionFact{
		item:        item,
		name:        name,
		contextName: typeName,
		contextKind: typeKind,
		lineNumber:  shared.NodeLine(phpNameNode(node)),
		parameters:  parameters,
		isPublic:    phpMethodIsPublic(node, state.source),
	})
	shared.AppendBucket(state.payload, "functions", item)
}

// recordPHPFunctionParameterTypes seeds the local variable type map for a
// function scope from its typed parameters and constructor property promotions.
func (state *phpParseState) recordPHPFunctionParameterTypes(node *tree_sitter.Node, typeName string, functionName string) {
	params := phpFormalParameters(node)
	if params == nil {
		return
	}
	scopeKey := phpFunctionScopeKey(typeName, functionName)
	cursor := params.Walk()
	defer cursor.Close()
	for _, param := range params.NamedChildren(cursor) {
		param := param
		switch param.Kind() {
		case "simple_parameter", "property_promotion_parameter", "variadic_parameter":
		default:
			continue
		}
		variable := phpParameterVariableName(&param, state.source)
		declaredType := phpTypeNodeName(phpMemberTypeNode(&param), state.source)
		if variable == "" || declaredType == "" {
			continue
		}
		normalized := normalizePHPImportedTypeName(declaredType, state.importAliases)
		if normalized == "" || normalized == "mixed" {
			continue
		}
		if state.localVariableTypes[scopeKey] == nil {
			state.localVariableTypes[scopeKey] = make(map[string]string)
		}
		state.localVariableTypes[scopeKey][variable] = normalized
		if param.Kind() == "property_promotion_parameter" && typeName != "" {
			if state.classPropertyTypes[typeName] == nil {
				state.classPropertyTypes[typeName] = make(map[string]string)
			}
			state.classPropertyTypes[typeName][variable] = normalized
		}
	}
}

func phpTypeItem(name string, node *tree_sitter.Node) map[string]any {
	return map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(phpNameNode(node)),
		"end_line":    shared.NodeLine(phpNameNode(node)),
		"lang":        "php",
	}
}

func phpDeclarationName(node *tree_sitter.Node, source []byte) string {
	return strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
}

func phpNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode
	}
	return node
}

func phpDeclarationList(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "declaration_list" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

func phpFormalParameters(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "formal_parameters" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}
