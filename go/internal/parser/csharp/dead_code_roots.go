package csharp

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type csharpTypeInfo struct {
	kind          string
	qualifiedName string
	bases         []string
}

type csharpSemanticFacts struct {
	types                     map[string]csharpTypeInfo
	typeSimpleNameCounts      map[string]int
	interfaceMethods          map[string]map[csharpMethodKey]struct{}
	interfaceSimpleNameCounts map[string]int
}

type csharpMethodKey struct {
	name  string
	arity int
}

type csharpMethodSyntax struct {
	attributes        []string
	modifiers         map[string]struct{}
	returnType        string
	parameterTypes    []string
	declarationHeader string
}

func (facts csharpSemanticFacts) typeInfo(simpleName string, qualifiedName string) csharpTypeInfo {
	if qualifiedName != "" {
		if info, ok := facts.types[qualifiedName]; ok {
			return info
		}
	}
	if facts.typeSimpleNameCounts[simpleName] == 1 {
		return facts.types[simpleName]
	}
	return csharpTypeInfo{}
}

func csharpContextTypeInfo(node *tree_sitter.Node, source []byte, simpleName string, qualifiedName string, facts csharpSemanticFacts) csharpTypeInfo {
	if info := facts.typeInfo(simpleName, qualifiedName); info.kind != "" {
		return info
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "struct_declaration", "record_declaration":
			name := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
			if name != simpleName {
				continue
			}
			currentQualifiedName := csharpQualifiedTypeName(current, source)
			if qualifiedName != "" && currentQualifiedName != "" && currentQualifiedName != qualifiedName {
				continue
			}
			return csharpTypeInfo{kind: current.Kind(), qualifiedName: currentQualifiedName, bases: csharpBaseNames(current, source)}
		}
	}
	return csharpTypeInfo{}
}

func csharpMethodKeyForNode(name string, node *tree_sitter.Node, source []byte) csharpMethodKey {
	syntax := csharpMethodSyntaxForNode(node, source)
	return csharpMethodKey{name: name, arity: len(syntax.parameterTypes)}
}

func csharpBaseNames(node *tree_sitter.Node, source []byte) []string {
	baseListNode := csharpBaseListNode(node)
	if baseListNode == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var bases []string
	cursor := baseListNode.Walk()
	defer cursor.Close()
	for _, child := range baseListNode.NamedChildren(cursor) {
		child := child
		name := strings.TrimSpace(csharpTypeNameFromNode(&child, source))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		bases = append(bases, name)
	}
	slices.Sort(bases)
	return bases
}

func csharpBaseListNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if baseListNode := node.ChildByFieldName("bases"); baseListNode != nil {
		return baseListNode
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "base_list" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

func nearestNamedAncestorWithQualifiedKind(node *tree_sitter.Node, source []byte, kinds ...string) (string, string, string) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		for _, kind := range kinds {
			if current.Kind() != kind {
				continue
			}
			name := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
			return name, kind, csharpQualifiedTypeName(current, source)
		}
	}
	return "", "", ""
}

func collectCSharpSemanticFacts(root *tree_sitter.Node, source []byte) csharpSemanticFacts {
	facts := csharpSemanticFacts{
		types:                     map[string]csharpTypeInfo{},
		typeSimpleNameCounts:      map[string]int{},
		interfaceMethods:          map[string]map[csharpMethodKey]struct{}{},
		interfaceSimpleNameCounts: map[string]int{},
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_declaration", "interface_declaration", "struct_declaration", "record_declaration":
			name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
			if name == "" {
				return
			}
			qualifiedName := csharpQualifiedTypeName(node, source)
			info := csharpTypeInfo{kind: node.Kind(), qualifiedName: qualifiedName, bases: csharpBaseNames(node, source)}
			facts.typeSimpleNameCounts[name]++
			if facts.typeSimpleNameCounts[name] == 1 {
				facts.types[name] = info
			} else {
				delete(facts.types, name)
			}
			if qualifiedName != "" && qualifiedName != name {
				facts.types[qualifiedName] = info
			}
			if node.Kind() == "interface_declaration" {
				facts.interfaceSimpleNameCounts[name]++
			}
		}
	})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		contextName, contextKind, contextQualified := nearestNamedAncestorWithQualifiedKind(node, source, "interface_declaration")
		if contextKind != "interface_declaration" || contextName == "" {
			return
		}
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		methodKey := csharpMethodKeyForNode(name, node, source)
		if contextQualified != "" {
			csharpAddInterfaceMethod(facts.interfaceMethods, contextQualified, methodKey)
		}
		if facts.interfaceSimpleNameCounts[contextName] == 1 {
			csharpAddInterfaceMethod(facts.interfaceMethods, contextName, methodKey)
		}
	})
	return facts
}

func csharpFunctionRootKinds(
	node *tree_sitter.Node,
	source []byte,
	name string,
	contextName string,
	contextKind string,
	contextQualified string,
	facts csharpSemanticFacts,
) []string {
	var rootKinds []string
	syntax := csharpMethodSyntaxForNode(node, source)
	typeInfo := csharpContextTypeInfo(node, source, contextName, contextQualified, facts)
	methodKey := csharpMethodKey{name: name, arity: len(syntax.parameterTypes)}
	if csharpIsMainMethod(node, name, syntax) {
		rootKinds = append(rootKinds, "csharp.main_method")
	}
	if node.Kind() == "constructor_declaration" {
		rootKinds = append(rootKinds, "csharp.constructor")
	}
	if contextKind == "interface_declaration" {
		rootKinds = append(rootKinds, "csharp.interface_method")
	}
	if csharpMethodImplementsLocalInterface(methodKey, typeInfo, facts) {
		rootKinds = append(rootKinds, "csharp.interface_implementation_method")
	}
	if syntax.hasModifier("override") {
		rootKinds = append(rootKinds, "csharp.override_method")
	}
	if csharpIsASPNetControllerAction(name, contextName, typeInfo, syntax) {
		rootKinds = append(rootKinds, "csharp.aspnet_controller_action")
	}
	if csharpIsHostedServiceEntrypoint(name, typeInfo) {
		rootKinds = append(rootKinds, "csharp.hosted_service_entrypoint")
	}
	if csharpAttributesContainAny(syntax.attributes, "Fact", "Theory", "Test", "TestMethod", "SetUp", "TearDown", "OneTimeSetUp", "OneTimeTearDown") {
		rootKinds = append(rootKinds, "csharp.test_method")
	}
	if csharpAttributesContainAny(syntax.attributes, "OnSerializing", "OnSerialized", "OnDeserializing", "OnDeserialized") {
		rootKinds = append(rootKinds, "csharp.serialization_callback")
	}
	return shared.DedupeNonEmptyStrings(rootKinds)
}

func csharpMethodSyntaxForNode(node *tree_sitter.Node, source []byte) csharpMethodSyntax {
	syntax := csharpMethodSyntax{modifiers: map[string]struct{}{}}
	if node == nil {
		return syntax
	}
	syntax.declarationHeader = csharpDeclarationHeader(shared.NodeText(node, source))
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		syntax.returnType = strings.TrimSpace(shared.NodeText(typeNode, source))
	}
	if parametersNode := node.ChildByFieldName("parameters"); parametersNode != nil {
		syntax.parameterTypes = csharpParameterTypes(parametersNode, source)
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "attribute_list":
			syntax.attributes = append(syntax.attributes, csharpAttributeNamesFromList(&child, source)...)
		case "modifier":
			for _, modifier := range strings.Fields(strings.ToLower(shared.NodeText(&child, source))) {
				syntax.modifiers[modifier] = struct{}{}
			}
		}
	}
	syntax.attributes = shared.DedupeNonEmptyStrings(syntax.attributes)
	return syntax
}

func csharpParameterTypes(parametersNode *tree_sitter.Node, source []byte) []string {
	var parameterTypes []string
	cursor := parametersNode.Walk()
	defer cursor.Close()
	for _, child := range parametersNode.NamedChildren(cursor) {
		child := child
		if child.Kind() != "parameter" {
			continue
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		parameterTypes = append(parameterTypes, strings.TrimSpace(shared.NodeText(typeNode, source)))
	}
	return parameterTypes
}

func csharpAddInterfaceMethod(methods map[string]map[csharpMethodKey]struct{}, interfaceName string, methodKey csharpMethodKey) {
	if methods[interfaceName] == nil {
		methods[interfaceName] = map[csharpMethodKey]struct{}{}
	}
	methods[interfaceName][methodKey] = struct{}{}
}

func csharpMethodImplementsLocalInterface(methodKey csharpMethodKey, typeInfo csharpTypeInfo, facts csharpSemanticFacts) bool {
	for _, base := range typeInfo.bases {
		if csharpInterfaceMethodsContain(facts.interfaceMethods[base], methodKey) {
			return true
		}
		simpleName := csharpLastTypeSegment(base)
		if facts.interfaceSimpleNameCounts[simpleName] != 1 {
			continue
		}
		if csharpInterfaceMethodsContain(facts.interfaceMethods[simpleName], methodKey) {
			return true
		}
	}
	return false
}

func csharpInterfaceMethodsContain(methods map[csharpMethodKey]struct{}, methodKey csharpMethodKey) bool {
	if methods == nil {
		return false
	}
	_, ok := methods[methodKey]
	return ok
}

func csharpIsMainMethod(node *tree_sitter.Node, name string, syntax csharpMethodSyntax) bool {
	if name != "Main" || node.Kind() == "local_function_statement" || !syntax.hasModifier("static") {
		return false
	}
	returnType, parameterTypes := csharpMainSignatureParts(syntax)
	switch csharpNormalizedType(returnType) {
	case "void", "int", "task", "task<int>", "system.threading.tasks.task", "system.threading.tasks.task<int>":
	default:
		return false
	}
	if len(parameterTypes) == 0 {
		return true
	}
	return len(parameterTypes) == 1 && csharpIsMainStringArrayParameter(parameterTypes[0])
}

func csharpIsMainStringArrayParameter(parameterType string) bool {
	switch csharpNormalizedType(parameterType) {
	case "string[]", "system.string[]":
		return true
	default:
		return false
	}
}
