// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse reads path and returns the JavaScript-family parser payload.
// Parse-level shared types (ParserFactory, jsParseByteCap, bounded-file
// events) live in javascript_language_helpers.go.
// parserReturner must be the paired return function for parserFactory; the
// borrowed parser is returned to the pool via parserReturner instead of
// calling parser.Close directly.
func Parse(
	parserFactory ParserFactory,
	parserReturner ParserReturner,
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
	defer parserReturner(runtimeLanguage, parser)

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, outputLanguage, isDependency)
	payload["components"] = []map[string]any{}
	payload["js_parse_bounded"] = []map[string]any{}

	if len(source) > jsParseByteCap {
		recordJSBoundedFile(payload, path, len(source))
		payload[fingerprint.StatsKey] = (&fingerprint.Stats{}).Map()
		return payload, nil
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s file %q: parser returned nil tree", outputLanguage, path)
	}
	defer tree.Close()
	if outputLanguage != "javascript" {
		payload["interfaces"] = []map[string]any{}
		payload["type_aliases"] = []map[string]any{}
		payload["enums"] = []map[string]any{}
	}
	scope := options.NormalizedVariableScope()
	root := tree.RootNode()
	// Fingerprint state for the #6833 code-divergence report: error graphs
	// are excluded per the #6834 verdict, and per-file outcomes accumulate
	// for collector-side telemetry via payload[fingerprint.StatsKey].
	fpStats := &fingerprint.Stats{}
	fpHasError := root.HasError()
	parents := syntax.BuildParentLookup(root)
	sourceText := string(source)
	payload["embedded_shell_commands"] = embeddedShellCommandPayloads(root, source, outputLanguage)
	rootIndexes := buildJavaScriptRootIndexes(root, source, sourceText, outputLanguage)
	reactAliases := rootIndexes.reactAliases
	siblingParser := newJavaScriptSiblingParser(parserFactory, parserReturner)
	defer siblingParser.Close()
	deadCodeRoots := javaScriptDeadCodeRootEvidence(repoRoot, path, root, source, siblingParser, parents, rootIndexes.fastifyBases, rootIndexes.expressBases, rootIndexes.koaBases)
	if len(deadCodeRoots.fileRootKinds) > 0 {
		payload["dead_code_file_root_kinds"] = append([]string(nil), deadCodeRoots.fileRootKinds...)
	}
	commonJSModuleAliases := rootIndexes.commonJSModuleAliases
	tsConfigImports := project.NewTSConfigImportResolver(repoRoot, path)
	newExpressionTypes := rootIndexes.newExpressionTypes
	fastifyBases := rootIndexes.fastifyBases

	// Gate the gather on framework presence. Non-framework files (the
	// overwhelming majority of real JS/TS) must not pay the clone+append
	// cost that only framework files benefit from. The bases are already
	// computed in buildJavaScriptRootIndexes; each base being non-empty
	// implies the file imports that framework. NestJS is import-gated
	// directly since it uses decorators, not registration-base variables.
	wantFrameworkGather := len(fastifyBases) > 0 ||
		len(rootIndexes.expressBases) > 0 ||
		len(rootIndexes.koaBases) > 0 ||
		javaScriptHasNestJSCommonImport(sourceText)

	// Gather resolution-candidate node pointers during the declaration walk
	// so the post-walk framework-semantics resolution can iterate them
	// in-memory instead of re-walking the entire tree per framework.
	var gatheredCallExpressions []*tree_sitter.Node
	var gatheredMethodDefinitions []*tree_sitter.Node

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "generator_function_declaration":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
			maybeAppendJavaScriptComponent(payload, node, nameNode, source, outputLanguage, reactAliases)
		case "method_definition":
			nameNode := node.ChildByFieldName("name")
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
			if wantFrameworkGather {
				gatheredMethodDefinitions = append(gatheredMethodDefinitions, cloneNode(node))
			}
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
				classItem["decorators"] = javaScriptDecorators(node, source, parents)
				classItem["type_parameters"] = javaScriptTypeParameters(node, source)
				if interfaces := syntax.ImplementedInterfaces(node, source); len(interfaces) > 0 {
					classItem["implemented_interfaces"] = interfaces
				}
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
				appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
				maybeAppendJavaScriptComponent(payload, valueNode, nameNode, source, outputLanguage, reactAliases)
				return
			}
			if outputLanguage == "tsx" && javaScriptComponentWrapperKind(valueNode, source, reactAliases) != "" {
				maybeAppendJavaScriptComponent(payload, valueNode, nameNode, source, outputLanguage, reactAliases)
			}
			if scope == "module" && javaScriptInsideFunction(node, parents) {
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
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
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
			if wantFrameworkGather {
				gatheredCallExpressions = append(gatheredCallExpressions, cloneNode(node))
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
				if classContext := javaScriptEnclosingClassName(node, source, parents); classContext != "" {
					item["class_context"] = classContext
				}
			}
			appendBucket(payload, "function_calls", item)
			for _, reference := range javaScriptFunctionValueReferenceCalls(
				node,
				source,
				outputLanguage,
				commonJSModuleAliases,
				fastifyBases,
				parents,
			) {
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
			for _, reference := range javaScriptFunctionValueReferenceCallsFromArguments(
				node.ChildByFieldName("arguments"),
				source,
				outputLanguage,
				commonJSModuleAliases,
				false,
				parents,
			) {
				appendBucket(payload, "function_calls", reference)
			}
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
			appendFunctionDeclaration(payload, path, node, nameNode, source, outputLanguage, options, deadCodeRoots, fpHasError, fpStats)
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

	syntax.AppendTypeReferenceCalls(payload, root, source, outputLanguage)
	annotateTypeScriptDeclarationMerges(payload, outputLanguage)
	sortNamedBucket(payload, "functions")
	payload[fingerprint.StatsKey] = fpStats.Map()
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
	payload["framework_semantics"] = buildJavaScriptFrameworkSemantics(
		path, root, source, payload, parents,
		fastifyBases, rootIndexes.expressBases, rootIndexes.koaBases,
		gatheredCallExpressions, gatheredMethodDefinitions,
	)

	emitValueFlowBuckets(payload, root, source, outputLanguage, options)

	return payload, nil
}

// PreScan returns JavaScript-family symbols used by repository pre-scan.
// parserReturner must be the paired return function for parserFactory.
func PreScan(
	parserFactory ParserFactory,
	parserReturner ParserReturner,
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	return preScanNames(parserFactory, parserReturner, path, runtimeLanguage, outputLanguage)
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
	fpHasError bool,
	fpStats *fingerprint.Stats,
) {
	name := syntax.FunctionName(nameNode, source)
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
	// CommonJS export assignments (`module.exports = function ...`,
	// `exports.foo = (...) => ...`) carry the function under `right`, not
	// `body`: without this unwrap the call below fingerprints the
	// assignment node itself and records no_body for every exported
	// function.
	if node != nil && node.Kind() == "assignment_expression" {
		if valueNode := node.ChildByFieldName("right"); isJavaScriptFunctionValue(valueNode) {
			declarationNode = valueNode
		}
	}

	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(nameNode),
		"end_line":        nodeEndLine(declarationNode),
		"decorators":      javaScriptDecorators(declarationNode, source, deadCodeRoots.parents),
		"type_parameters": javaScriptTypeParameters(declarationNode, source),
		"parameter_count": syntax.ParameterCount(declarationNode.ChildByFieldName("parameters"), source),
		"lang":            lang,
	}
	if rootKinds := javaScriptDeadCodeRootKinds(path, node, name, source, deadCodeRoots); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if functionType := syntax.FunctionKind(declarationNode, source); functionType != "" {
		item["type"] = functionType
		if functionType == "generator" {
			item["semantic_kind"] = "generator"
		}
	}
	if docstring := syntax.Docstring(declarationNode, source); docstring != "" {
		item["docstring"] = docstring
	}
	for key, value := range javaScriptFunctionSemantics(declarationNode, source, lang, deadCodeRoots.parents) {
		item[key] = value
	}
	if options.IndexSource {
		item["source"] = nodeText(declarationNode, source)
	}
	if declarationNode != nil {
		fingerprint.Attach(lang, fpHasError, declarationNode.ChildByFieldName("body"), source, item, fpStats)
	} else {
		fpStats.Record(fingerprint.ReasonNoBody, 0)
	}
	appendBucket(payload, "functions", item)
}
