package csharp

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

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
