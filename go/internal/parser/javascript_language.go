package parser

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseJavaScriptLike(
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser(runtimeLanguage)
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s file %q: parser returned nil tree", outputLanguage, path)
	}
	defer tree.Close()

	payload := basePayload(path, outputLanguage, isDependency)
	payload["components"] = []map[string]any{}
	if outputLanguage != "javascript" {
		payload["interfaces"] = []map[string]any{}
		payload["type_aliases"] = []map[string]any{}
		payload["enums"] = []map[string]any{}
	}
	scope := options.normalizedVariableScope()
	root := tree.RootNode()
	reactAliases := javaScriptReactAliases(root, source, outputLanguage)
	deadCodeRoots := javaScriptDeadCodeRootEvidence(repoRoot, path, root, source)
	if len(deadCodeRoots.fileRootKinds) > 0 {
		payload["dead_code_file_root_kinds"] = append([]string(nil), deadCodeRoots.fileRootKinds...)
	}
	tsConfigImports := newJavaScriptTSConfigImportResolver(repoRoot, path)
	newExpressionTypes := javaScriptNewExpressionVariableTypes(root, source)

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "generator_function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "method_definition", "method_signature":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
		case "class_declaration", "abstract_class_declaration":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			classItem := map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        outputLanguage,
			}
			if outputLanguage != "javascript" {
				classItem["decorators"] = javaScriptDecorators(node, source)
				classItem["type_parameters"] = javaScriptTypeParameters(node, source)
			}
			if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, source, deadCodeRoots); len(rootKinds) > 0 {
				classItem["dead_code_root_kinds"] = rootKinds
			}
			appendBucket(payload, "classes", classItem)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "interface_declaration":
			if outputLanguage == "javascript" {
				return
			}
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        outputLanguage,
			}
			if outputLanguage != "javascript" {
				item["type_parameters"] = javaScriptTypeParameters(node, source)
			}
			if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, source, deadCodeRoots); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			appendBucket(payload, "interfaces", item)
		case "type_alias_declaration":
			if outputLanguage == "javascript" {
				return
			}
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "type_aliases", javaScriptTypeAliasItem(node, nameNode, source, outputLanguage))
		case "enum_declaration":
			if outputLanguage == "javascript" {
				return
			}
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "enums", map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        outputLanguage,
			})
		case "variable_declarator":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			valueNode := node.ChildByFieldName("value")
			if isJavaScriptFunctionValue(valueNode) {
				appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
				maybeAppendJavaScriptComponent(payload, valueNode, nameNode, source, outputLanguage, reactAliases)
				return
			}
			if outputLanguage == "tsx" && javaScriptComponentWrapperKind(valueNode, source, reactAliases) != "" {
				maybeAppendJavaScriptComponent(payload, valueNode, nameNode, source, outputLanguage, reactAliases)
			}
			if scope == "module" && javaScriptInsideFunction(node) {
				return
			}
			if requireItems := javaScriptRequireImportEntries(node, source, outputLanguage); len(requireItems) > 0 {
				for _, item := range requireItems {
					tsConfigImports.annotateImport(item)
					appendBucket(payload, "imports", item)
				}
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        outputLanguage,
			}
			if outputLanguage == "tsx" {
				if assertion := javaScriptComponentTypeAssertion(valueNode, source, reactAliases); assertion != "" {
					item["component_type_assertion"] = assertion
				} else if typeNode := node.ChildByFieldName("type"); typeNode != nil {
					if assertion := javaScriptComponentTypeAssertion(typeNode, source, reactAliases); assertion != "" {
						item["component_type_assertion"] = assertion
					}
				} else if assertion := javaScriptComponentTypeAssertion(node, source, reactAliases); assertion != "" {
					item["component_type_assertion"] = assertion
				}
			}
			appendBucket(payload, "variables", item)
		case "import_statement":
			for _, item := range javaScriptImportEntries(node, source, outputLanguage) {
				tsConfigImports.annotateImport(item)
				appendBucket(payload, "imports", item)
			}
		case "export_statement":
			for _, item := range javaScriptReExportEntries(node, source, outputLanguage) {
				tsConfigImports.annotateImport(item)
				appendBucket(payload, "imports", item)
			}
		case "call_expression":
			functionNode := node.ChildByFieldName("function")
			name := javaScriptCallName(functionNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"full_name":   javaScriptCallFullName(functionNode, source),
				"call_kind":   "function_call",
				"line_number": nodeLine(node),
				"lang":        outputLanguage,
			}
			if inferredType := javaScriptCallInferredObjectType(functionNode, source, newExpressionTypes); inferredType != "" {
				item["inferred_obj_type"] = inferredType
			}
			appendBucket(payload, "function_calls", item)
		case "new_expression":
			constructorName, constructorFullName := javaScriptNewExpressionConstructorName(node, source)
			if constructorName == "" {
				return
			}
			appendBucket(payload, "function_calls", map[string]any{
				"name":        constructorName,
				"full_name":   constructorFullName,
				"call_kind":   "constructor_call",
				"line_number": nodeLine(node),
				"lang":        outputLanguage,
			})
		case "assignment_expression":
			leftNode := node.ChildByFieldName("left")
			rightNode := node.ChildByFieldName("right")
			if !isJavaScriptFunctionValue(rightNode) {
				return
			}
			nameNode := javaScriptExportAssignmentNameNode(leftNode, source)
			if nameNode == nil {
				return
			}
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
		case "jsx_opening_element", "jsx_self_closing_element":
			if outputLanguage != "tsx" {
				return
			}
			nameNode := node.ChildByFieldName("name")
			name := javaScriptJSXComponentName(node, source)
			if !isPascalIdentifier(name) {
				return
			}
			appendBucket(payload, "function_calls", map[string]any{
				"name":        name,
				"full_name":   javaScriptCallFullName(nameNode, source),
				"call_kind":   "jsx_component",
				"line_number": nodeLine(node),
				"lang":        outputLanguage,
			})
		case "internal_module":
			if outputLanguage != "typescript" {
				return
			}
			if item := javaScriptNamespaceModuleItem(node, source, outputLanguage); item != nil {
				appendBucket(payload, "modules", item)
			}
		}
	})

	annotateTypeScriptDeclarationMerges(payload, outputLanguage)
	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "modules")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	sortNamedBucket(payload, "components")
	if outputLanguage != "javascript" {
		sortNamedBucket(payload, "interfaces")
		sortNamedBucket(payload, "type_aliases")
		sortNamedBucket(payload, "enums")
	}
	payload["framework_semantics"] = buildJavaScriptFrameworkSemantics(path, source, payload)

	return payload, nil
}

func (e *Engine) preScanJavaScriptLike(
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	payload, err := e.parseJavaScriptLike(repoRoot, path, runtimeLanguage, outputLanguage, false, Options{})
	if err != nil {
		return nil, err
	}
	keys := []string{"functions", "classes"}
	if outputLanguage != "javascript" {
		keys = append(keys, "interfaces")
	}
	names := collectBucketNames(payload, keys...)
	slices.Sort(names)
	return names, nil
}

func appendFunctionDeclaration(
	payload map[string]any,
	path string,
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	source []byte,
	lang string,
	options Options,
	deadCodeRoots javaScriptDeadCodeEvidence,
) {
	name := javaScriptFunctionName(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	declarationNode := node
	if node != nil && node.Kind() == "variable_declarator" {
		if valueNode := node.ChildByFieldName("value"); isJavaScriptFunctionValue(valueNode) {
			declarationNode = valueNode
		}
	}

	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(nameNode),
		"end_line":        nodeEndLine(declarationNode),
		"decorators":      javaScriptDecorators(declarationNode, source),
		"type_parameters": javaScriptTypeParameters(declarationNode, source),
		"lang":            lang,
	}
	if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, source, deadCodeRoots); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if functionType := javaScriptFunctionKind(declarationNode, source); functionType != "" {
		item["type"] = functionType
		if functionType == "generator" {
			item["semantic_kind"] = "generator"
		}
	}
	if docstring := javaScriptDocstring(declarationNode, source); docstring != "" {
		item["docstring"] = docstring
	}
	for key, value := range javaScriptFunctionSemantics(declarationNode, source, lang) {
		item[key] = value
	}
	if options.IndexSource {
		item["source"] = nodeText(declarationNode, source)
	}
	appendBucket(payload, "functions", item)
}

func isJavaScriptFunctionValue(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "function_expression", "arrow_function", "generator_function", "generator_function_declaration":
		return true
	default:
		return false
	}
}

func javaScriptInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_declaration", "function_expression", "arrow_function", "method_definition":
			return true
		}
	}
	return false
}

func javaScriptDecorators(node *tree_sitter.Node, source []byte) []string {
	decorators := make([]string, 0)
	for current := node; current != nil; current = current.Parent() {
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if child.Kind() != "decorator" {
				continue
			}
			decorator := strings.TrimSpace(nodeText(&child, source))
			if decorator == "" {
				continue
			}
			decorators = append(decorators, decorator)
		}
		cursor.Close()
		if current.Kind() == "decorated_definition" {
			return decorators
		}
		if current.Parent() == nil || current.Parent().Kind() != "decorated_definition" {
			break
		}
	}
	return decorators
}

func javaScriptTypeParameters(node *tree_sitter.Node, source []byte) []string {
	return javaScriptTypeParameterNames(nodeText(node, source))
}

func javaScriptCallName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			if name := javaScriptCallName(&children[i], source); name != "" {
				return name
			}
		}
	case "identifier":
		return nodeText(node, source)
	case "member_expression":
		property := node.ChildByFieldName("property")
		return nodeText(property, source)
	default:
		return ""
	}
	return ""
}

func javaScriptCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(node, source))
}

func javaScriptJSXComponentName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	switch nameNode.Kind() {
	case "identifier", "property_identifier", "jsx_identifier", "type_identifier":
		return strings.TrimSpace(nodeText(nameNode, source))
	case "member_expression", "nested_identifier":
		propertyNode := nameNode.ChildByFieldName("property")
		if propertyNode != nil {
			return strings.TrimSpace(nodeText(propertyNode, source))
		}
		text := strings.TrimSpace(nodeText(nameNode, source))
		if text == "" {
			return ""
		}
		parts := strings.Split(text, ".")
		return strings.TrimSpace(parts[len(parts)-1])
	default:
		return ""
	}
}
