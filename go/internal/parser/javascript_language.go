package parser

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseJavaScriptLike(
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
	registeredRootKinds := javaScriptRegisteredDeadCodeRootKinds(root, source)

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, registeredRootKinds)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "generator_function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, registeredRootKinds)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "method_definition", "method_signature":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, registeredRootKinds)
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
				appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, registeredRootKinds)
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
				appendBucket(payload, "imports", item)
			}
		case "call_expression":
			functionNode := node.ChildByFieldName("function")
			name := javaScriptCallName(functionNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "function_calls", map[string]any{
				"name":        name,
				"full_name":   javaScriptCallFullName(functionNode, source),
				"call_kind":   "function_call",
				"line_number": nodeLine(node),
				"lang":        outputLanguage,
			})
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
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	payload, err := e.parseJavaScriptLike(path, runtimeLanguage, outputLanguage, false, Options{})
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
	registeredRootKinds map[string][]string,
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
	if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, registeredRootKinds); len(rootKinds) > 0 {
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
	for key, value := range javaScriptFunctionSemantics(declarationNode, lang) {
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

func javaScriptImportEntries(node *tree_sitter.Node, source []byte, lang string) []map[string]any {
	sourceNode := node.ChildByFieldName("source")
	moduleSource := strings.Trim(nodeText(sourceNode, source), `"'`)
	if strings.TrimSpace(moduleSource) == "" {
		return nil
	}

	importNode := node.ChildByFieldName("import")
	if importNode == nil {
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if child.Kind() == "string" {
				continue
			}
			importNode = &child
			break
		}
	}
	if importNode == nil {
		return []map[string]any{{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": nodeLine(sourceNode),
			"lang":        lang,
		}}
	}

	items := make([]map[string]any, 0)
	cursor := importNode.Walk()
	defer cursor.Close()
	children := importNode.NamedChildren(cursor)
	if len(children) == 0 {
		children = []tree_sitter.Node{*importNode}
	}
	for _, child := range children {
		child := child
		switch child.Kind() {
		case "import_clause":
			clauseCursor := child.Walk()
			defer clauseCursor.Close()
			for _, clauseChild := range child.NamedChildren(clauseCursor) {
				clauseChild := clauseChild
				items = append(items, javaScriptImportEntriesFromClause(&clauseChild, moduleSource, source, lang)...)
			}
		case "identifier":
			items = append(items, javaScriptImportEntriesFromClause(&child, moduleSource, source, lang)...)
		case "namespace_import", "named_imports":
			items = append(items, javaScriptImportEntriesFromClause(&child, moduleSource, source, lang)...)
		}
	}
	if len(items) == 0 {
		items = append(items, map[string]any{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": nodeLine(sourceNode),
			"lang":        lang,
		})
	}
	return items
}

func javaScriptImportEntriesFromClause(
	node *tree_sitter.Node,
	moduleSource string,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil {
		return nil
	}

	switch node.Kind() {
	case "identifier":
		return []map[string]any{{
			"name":        "default",
			"source":      moduleSource,
			"alias":       nodeText(node, source),
			"line_number": nodeLine(node),
			"lang":        lang,
		}}
	case "namespace_import":
		aliasNode := node.ChildByFieldName("name")
		return []map[string]any{{
			"name":        "*",
			"source":      moduleSource,
			"alias":       nodeText(aliasNode, source),
			"line_number": nodeLine(node),
			"lang":        lang,
		}}
	case "named_imports":
		items := make([]map[string]any, 0)
		cursor := node.Walk()
		defer cursor.Close()
		for _, specifier := range node.NamedChildren(cursor) {
			specifier := specifier
			if specifier.Kind() != "import_specifier" {
				continue
			}
			nameNode := specifier.ChildByFieldName("name")
			aliasNode := specifier.ChildByFieldName("alias")
			items = append(items, map[string]any{
				"name":        nodeText(nameNode, source),
				"source":      moduleSource,
				"alias":       nodeText(aliasNode, source),
				"line_number": nodeLine(&specifier),
				"lang":        lang,
			})
		}
		return items
	default:
		return nil
	}
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
