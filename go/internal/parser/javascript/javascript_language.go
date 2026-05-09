package javascript

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ParserFactory returns a fresh tree-sitter parser for the requested runtime
// grammar name. The parent parser owns runtime caching; this package owns the
// language adapter once a parser is provided.
type ParserFactory func(language string) (*tree_sitter.Parser, error)

// Parse reads path and returns the JavaScript-family parser payload.
func Parse(
	parserFactory ParserFactory,
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
	isDependency bool,
	options shared.Options,
) (map[string]any, error) {
	parser, err := parserFactory(runtimeLanguage)
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
	scope := options.NormalizedVariableScope()
	root := tree.RootNode()
	reactAliases := javaScriptReactAliases(root, source, outputLanguage)
	deadCodeRoots := javaScriptDeadCodeRootEvidence(repoRoot, path, root, source)
	if len(deadCodeRoots.fileRootKinds) > 0 {
		payload["dead_code_file_root_kinds"] = append([]string(nil), deadCodeRoots.fileRootKinds...)
	}
	commonJSModuleAliases := javaScriptCommonJSModuleExportAliases(root, source)
	tsConfigImports := NewTSConfigImportResolver(repoRoot, path)
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
		case "method_definition":
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
			appendBucket(payload, "type_aliases", javaScriptTypeAliasItem(node, nameNode, source, outputLanguage, deadCodeRoots))
		case "enum_declaration":
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
			if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, source, deadCodeRoots); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			appendBucket(payload, "enums", item)
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
					annotateJavaScriptResolvedImport(item, tsConfigImports)
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
		case "pair":
			nameNode := node.ChildByFieldName("key")
			valueNode := node.ChildByFieldName("value")
			if !isJavaScriptFunctionValue(valueNode) {
				if item := javaScriptHapiRouteHandlerReferenceCall(node, nameNode, valueNode, source, outputLanguage, deadCodeRoots); item != nil {
					appendBucket(payload, "function_calls", item)
				}
				return
			}
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots)
		case "import_statement":
			for _, item := range javaScriptImportEntries(node, source, outputLanguage) {
				annotateJavaScriptResolvedImport(item, tsConfigImports)
				appendBucket(payload, "imports", item)
			}
		case "export_statement":
			for _, item := range javaScriptReExportEntries(node, source, outputLanguage) {
				annotateJavaScriptResolvedImport(item, tsConfigImports)
				appendBucket(payload, "imports", item)
			}
		case "call_expression":
			functionNode := node.ChildByFieldName("function")
			name := javaScriptCallName(functionNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			fullName := rewriteJavaScriptCommonJSModuleExportAliasFullName(
				javaScriptCallFullName(functionNode, source),
				commonJSModuleAliases,
			)
			item := map[string]any{
				"name":        name,
				"full_name":   fullName,
				"call_kind":   "function_call",
				"line_number": nodeLine(node),
				"lang":        outputLanguage,
			}
			if inferredType := javaScriptCallInferredObjectType(functionNode, source, newExpressionTypes); inferredType != "" {
				item["inferred_obj_type"] = inferredType
			}
			if strings.HasPrefix(fullName, "this.") {
				if classContext := javaScriptEnclosingClassName(node, source); classContext != "" {
					item["class_context"] = classContext
				}
			}
			appendBucket(payload, "function_calls", item)
			for _, reference := range javaScriptFunctionValueReferenceCalls(node, source, outputLanguage, commonJSModuleAliases) {
				appendBucket(payload, "function_calls", reference)
			}
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
		case "return_statement":
			valueNode := javaScriptReturnValueNode(node)
			if item := javaScriptFunctionValueReferenceCall(valueNode, source, outputLanguage, commonJSModuleAliases); item != nil {
				appendBucket(payload, "function_calls", item)
			}
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

	appendJavaScriptTypeReferenceCalls(payload, root, source, outputLanguage)
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

// PreScan returns JavaScript-family symbols used by repository pre-scan.
func PreScan(
	parserFactory ParserFactory,
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	payload, err := Parse(parserFactory, repoRoot, path, runtimeLanguage, outputLanguage, false, shared.Options{})
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
	options shared.Options,
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
	if node != nil && node.Kind() == "pair" {
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
