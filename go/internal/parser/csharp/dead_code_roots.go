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
	interfaceMethods          map[string]map[string]struct{}
	interfaceSimpleNameCounts map[string]int
}

type csharpMethodSyntax struct {
	attributes        []string
	modifiers         map[string]struct{}
	returnType        string
	parameterTypes    []string
	declarationHeader string
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

func nearestNamedAncestorWithKind(node *tree_sitter.Node, source []byte, kinds ...string) (string, string) {
	name, kind, _ := nearestNamedAncestorWithQualifiedKind(node, source, kinds...)
	return name, kind
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
		interfaceMethods:          map[string]map[string]struct{}{},
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
			facts.types[name] = info
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
		if contextQualified != "" {
			csharpAddInterfaceMethod(facts.interfaceMethods, contextQualified, name)
		}
		if facts.interfaceSimpleNameCounts[contextName] == 1 {
			csharpAddInterfaceMethod(facts.interfaceMethods, contextName, name)
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
	facts csharpSemanticFacts,
) []string {
	var rootKinds []string
	syntax := csharpMethodSyntaxForNode(node, source)
	typeInfo := facts.types[contextName]
	if csharpIsMainMethod(node, name, syntax) {
		rootKinds = append(rootKinds, "csharp.main_method")
	}
	if node.Kind() == "constructor_declaration" {
		rootKinds = append(rootKinds, "csharp.constructor")
	}
	if contextKind == "interface_declaration" {
		rootKinds = append(rootKinds, "csharp.interface_method")
	}
	if csharpMethodImplementsLocalInterface(name, typeInfo, facts) {
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

func csharpAddInterfaceMethod(methods map[string]map[string]struct{}, interfaceName string, methodName string) {
	if methods[interfaceName] == nil {
		methods[interfaceName] = map[string]struct{}{}
	}
	methods[interfaceName][methodName] = struct{}{}
}

func csharpMethodImplementsLocalInterface(name string, typeInfo csharpTypeInfo, facts csharpSemanticFacts) bool {
	for _, base := range typeInfo.bases {
		if csharpInterfaceMethodsContain(facts.interfaceMethods[base], name) {
			return true
		}
		simpleName := csharpLastTypeSegment(base)
		if facts.interfaceSimpleNameCounts[simpleName] != 1 {
			continue
		}
		if csharpInterfaceMethodsContain(facts.interfaceMethods[simpleName], name) {
			return true
		}
	}
	return false
}

func csharpInterfaceMethodsContain(methods map[string]struct{}, name string) bool {
	if methods == nil {
		return false
	}
	_, ok := methods[name]
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
	return len(parameterTypes) == 1 && csharpNormalizedType(parameterTypes[0]) == "string[]"
}

func csharpIsASPNetControllerAction(name string, contextName string, typeInfo csharpTypeInfo, syntax csharpMethodSyntax) bool {
	if contextName == "" || name == contextName || !syntax.hasModifier("public") || csharpAttributesContainAny(syntax.attributes, "NonAction") {
		return false
	}
	if strings.HasSuffix(contextName, "Controller") {
		return true
	}
	for _, base := range typeInfo.bases {
		switch csharpLastTypeSegment(base) {
		case "Controller", "ControllerBase":
			return true
		}
	}
	return false
}

func csharpIsHostedServiceEntrypoint(name string, typeInfo csharpTypeInfo) bool {
	switch name {
	case "ExecuteAsync", "StartAsync", "StopAsync":
	default:
		return false
	}
	for _, base := range typeInfo.bases {
		switch csharpLastTypeSegment(base) {
		case "BackgroundService", "IHostedService":
			return true
		}
	}
	return false
}

func csharpAttributeNames(node *tree_sitter.Node, source []byte) []string {
	return csharpMethodSyntaxForNode(node, source).attributes
}

func csharpAttributeNamesFromList(node *tree_sitter.Node, source []byte) []string {
	var names []string
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if name := csharpAttributeNameFromNode(&child, source); name != "" {
			names = append(names, csharpLastTypeSegment(name))
		}
	}
	return names
}

func csharpAttributeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	return csharpTypeNameFromNode(node, source)
}

func csharpAttributesContainAny(attributes []string, names ...string) bool {
	for _, attribute := range attributes {
		normalized := strings.TrimSuffix(csharpLastTypeSegment(attribute), "Attribute")
		for _, name := range names {
			if normalized == strings.TrimSuffix(name, "Attribute") {
				return true
			}
		}
	}
	return false
}

func csharpTypeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "qualified_name", "generic_name":
		return strings.TrimSpace(shared.NodeText(node, source))
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if name := csharpTypeNameFromNode(&child, source); name != "" {
			return name
		}
	}
	return ""
}

func csharpQualifiedTypeName(node *tree_sitter.Node, source []byte) string {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return ""
	}
	var parents []string
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "namespace_declaration", "file_scoped_namespace_declaration",
			"class_declaration", "interface_declaration", "struct_declaration", "record_declaration":
			parentName := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
			if parentName != "" {
				parents = append(parents, parentName)
			}
		}
	}
	slices.Reverse(parents)
	parents = append(parents, name)
	return strings.Join(parents, ".")
}

func csharpLastTypeSegment(name string) string {
	name = strings.TrimSpace(name)
	for _, separator := range []string{".", ":"} {
		name = shared.LastPathSegment(name, separator)
	}
	if index := strings.Index(name, "<"); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

func (syntax csharpMethodSyntax) hasModifier(name string) bool {
	normalized := strings.ToLower(name)
	if _, ok := syntax.modifiers[normalized]; ok {
		return true
	}
	return csharpHeaderHasWord(syntax.declarationHeader, normalized)
}

func csharpNormalizedType(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "global::"))
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "\t", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.TrimPrefix(name, "System.")
	return strings.ToLower(name)
}

func csharpDeclarationHeader(text string) string {
	header := text
	for _, marker := range []string{"{", "=>"} {
		if index := strings.Index(header, marker); index >= 0 {
			header = header[:index]
		}
	}
	return header
}

func csharpMainSignatureParts(syntax csharpMethodSyntax) (string, []string) {
	if syntax.returnType != "" {
		return syntax.returnType, syntax.parameterTypes
	}
	header := csharpStripAttributes(syntax.declarationHeader)
	nameIndex := strings.Index(header, "Main")
	if nameIndex < 0 {
		return "", nil
	}
	prefix := strings.TrimSpace(header[:nameIndex])
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return "", nil
	}
	parameterText := ""
	if openIndex := strings.Index(header[nameIndex:], "("); openIndex >= 0 {
		start := nameIndex + openIndex + 1
		if closeIndex := strings.LastIndex(header, ")"); closeIndex >= start {
			parameterText = strings.TrimSpace(header[start:closeIndex])
		}
	}
	return fields[len(fields)-1], csharpSignatureParameterTypes(parameterText)
}

func csharpSignatureParameterTypes(parameterText string) []string {
	if parameterText == "" {
		return nil
	}
	parts := strings.Split(parameterText, ",")
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) < 2 {
			continue
		}
		types = append(types, strings.Join(fields[:len(fields)-1], " "))
	}
	return types
}

func csharpStripAttributes(text string) string {
	for {
		trimmed := strings.TrimSpace(text)
		if !strings.HasPrefix(trimmed, "[") {
			return trimmed
		}
		end := strings.Index(trimmed, "]")
		if end < 0 {
			return trimmed
		}
		text = trimmed[end+1:]
	}
}

func csharpHeaderHasWord(header string, word string) bool {
	header = csharpStripAttributes(header)
	for _, field := range strings.FieldsFunc(header, func(r rune) bool {
		return r != '_' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z')
	}) {
		if strings.EqualFold(field, word) {
			return true
		}
	}
	return false
}
