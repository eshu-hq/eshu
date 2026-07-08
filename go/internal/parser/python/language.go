// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Python declarations, imports, calls, framework semantics, and
// dead-code root metadata from .py and .ipynb inputs.
func Parse(
	repoRoot string,
	path string,
	isDependency bool,
	options shared.Options,
	parser *tree_sitter.Parser,
) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".ipynb") {
		tempPythonPath, err := convertNotebookToTempPython(path, source)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = os.Remove(tempPythonPath)
		}()
		source, err = readSource(tempPythonPath)
		if err != nil {
			return nil, err
		}
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse python file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "python", isDependency)
	payload["modules"] = []map[string]any{}
	payload["type_annotations"] = []map[string]any{}
	root := tree.RootNode()
	payload["embedded_shell_commands"] = embeddedShellCommandPayloads(root, source)
	scope := options.NormalizedVariableScope()
	lambdaHandlers := pythonLambdaHandlerRoots(repoRoot, path)
	primaryIndexes := buildPythonPrimaryIndexes(root, source)
	dataclassClasses := primaryIndexes.dataclassClasses
	scriptMainRoots := primaryIndexes.scriptMainRoots
	publicAPIRootKinds := pythonPublicAPIRootKinds(repoRoot, path, root, source, primaryIndexes.moduleAllNames)
	if docstring := pythonDocstring(root, source); docstring != "" {
		moduleName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if moduleName == "" {
			moduleName = filepath.Base(path)
		}
		appendBucket(payload, "modules", map[string]any{
			"name":        moduleName,
			"line_number": 1,
			"end_line":    1,
			"lang":        "python",
			"docstring":   docstring,
		})
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_definition":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        "python",
			}
			if docstring := pythonDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			decorators := pythonDecorators(node, source)
			if len(decorators) > 0 {
				item["decorators"] = decorators
			}
			if bases := pythonClassBaseNames(node, source); len(bases) > 0 {
				item["bases"] = bases
			}
			if metaclass := pythonClassMetaclass(node, source); metaclass != "" {
				item["metaclass"] = metaclass
			}
			if rootKinds := pythonClassDeadCodeRootKinds(decorators); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			if publicKinds := publicAPIRootKinds[name]; len(publicKinds) > 0 {
				item["dead_code_root_kinds"] = pythonMergeRootKinds(item["dead_code_root_kinds"], publicKinds)
			}
			if rationale := pythonRationaleComments(node, source); len(rationale) > 0 {
				item["rationale_comments"] = rationale
			}
			appendBucket(payload, "classes", item)
		case "function_definition":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			functionSource := nodeText(node, source)
			item := map[string]any{
				"name":                  name,
				"line_number":           nodeLine(nameNode),
				"end_line":              nodeEndLine(node),
				"args":                  pythonParameterNames(node.ChildByFieldName("parameters"), source),
				"lang":                  "python",
				"async":                 pythonFunctionIsAsync(functionSource),
				"cyclomatic_complexity": cyclomaticComplexity(node, source),
			}
			decorators := pythonDecorators(node, source)
			item["decorators"] = decorators
			classContext := pythonEnclosingClassName(node, source)
			if classContext != "" {
				item["class_context"] = classContext
			}
			rootKinds := pythonDeadCodeRootKinds(decorators)
			if scriptMainRoots[name] {
				rootKinds = appendUniqueString(rootKinds, "python.script_main_guard")
			}
			if lambdaHandlers.Has(path, name) {
				rootKinds = appendUniqueString(rootKinds, "python.aws_lambda_handler")
			}
			if name == "__post_init__" && dataclassClasses[classContext] {
				rootKinds = appendUniqueString(rootKinds, "python.dataclass_post_init")
			}
			switch {
			case classContext != "" && pythonIsClassProtocolMethod(name):
				rootKinds = appendUniqueString(rootKinds, "python.dunder_method")
			case classContext == "" && pythonIsModuleProtocolFunction(name):
				rootKinds = appendUniqueString(rootKinds, "python.dunder_method")
			case classContext == "" && pythonDunderFunctionAssignedInEnclosingScope(node, name, source):
				rootKinds = appendUniqueString(rootKinds, "python.dunder_method")
			}
			if classContext != "" && pythonPublicAPIClassMember(publicAPIRootKinds[classContext], name) {
				rootKinds = appendUniqueString(rootKinds, "python.public_api_member")
			}
			if publicKinds := publicAPIRootKinds[name]; len(publicKinds) > 0 {
				rootKinds = pythonMergeRootKinds(rootKinds, publicKinds)
			}
			if len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			if pythonFunctionIsGenerator(node) {
				item["semantic_kind"] = "generator"
			}
			if docstring := pythonDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			if rationale := pythonRationaleComments(node, source); len(rationale) > 0 {
				item["rationale_comments"] = rationale
			}
			if options.IndexSource {
				item["source"] = functionSource
			}
			appendBucket(payload, "functions", item)
			for _, annotation := range pythonTypeAnnotations(node, source, name) {
				appendBucket(payload, "type_annotations", annotation)
			}
		case "assignment":
			if lambdaItem, ok := pythonLambdaAssignmentItem(node, source, options); ok {
				appendBucket(payload, "functions", lambdaItem)
			}
			if scope == "module" && !pythonModuleScoped(node) {
				return
			}
			if annotationItem, ok := pythonAnnotatedAssignmentItem(node, source); ok {
				appendBucket(payload, "type_annotations", annotationItem)
			}
			left := node.ChildByFieldName("left")
			if left == nil || left.Kind() != "identifier" {
				return
			}
			name := nodeText(left, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(left),
				"end_line":    nodeEndLine(node),
				"lang":        "python",
			}
			if options.IndexSource {
				item["source"] = nodeText(node, source)
			}
			appendBucket(payload, "variables", item)
		case "import_statement":
			for _, item := range pythonImportEntries(path, node, source) {
				appendBucket(payload, "imports", item)
			}
		case "import_from_statement":
			for _, item := range pythonImportEntries(path, node, source) {
				appendBucket(payload, "imports", item)
			}
		case "call":
			function := node.ChildByFieldName("function")
			name := pythonCallName(function, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(node),
				"lang":        "python",
			}
			if fullName := pythonCallFullName(function, source); fullName != "" {
				item["full_name"] = fullName
				if pythonLooksLikeConstructor(fullName) {
					item["call_kind"] = "constructor_call"
				}
			}
			if inferredType := pythonCallInferredObjectType(node, function, source); inferredType != "" {
				item["inferred_obj_type"] = inferredType
			}
			appendBucket(payload, "function_calls", item)
			if classReference := pythonClassReferenceCallItem(function, node, source); classReference != nil {
				appendBucket(payload, "function_calls", classReference)
			}
		case "lambda":
			if lambdaItem, ok := pythonAnonymousLambdaItem(node, source, options); ok {
				appendBucket(payload, "functions", lambdaItem)
			}
		}
	})

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "modules")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	sortNamedBucket(payload, "type_annotations")
	fwGathered := gatherPythonFrameworkNodes(root)
	payload["framework_semantics"] = buildPythonFrameworkSemanticsGathered(fwGathered, root, source)
	payload["orm_table_mappings"] = buildPythonORMTableMappingsGathered(fwGathered.classes, source)

	emitValueFlowBuckets(payload, root, source, options)

	return payload, nil
}

// PreScan returns Python names used by the collector import-map pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	return preScanNames(path, parser)
}

func pythonCallName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return nodeText(node, source)
	case "attribute":
		attribute := node.ChildByFieldName("attribute")
		return nodeText(attribute, source)
	default:
		return ""
	}
}

func pythonCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return nodeText(node, source)
	case "attribute":
		object := node.ChildByFieldName("object")
		attribute := node.ChildByFieldName("attribute")
		objectName := pythonCallFullName(object, source)
		attributeName := nodeText(attribute, source)
		if strings.TrimSpace(objectName) == "" {
			return attributeName
		}
		if strings.TrimSpace(attributeName) == "" {
			return objectName
		}
		return objectName + "." + attributeName
	default:
		return nodeText(node, source)
	}
}

func pythonDecorators(node *tree_sitter.Node, source []byte) []string {
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

func pythonFunctionIsAsync(functionSource string) bool {
	return strings.HasPrefix(strings.TrimSpace(functionSource), "async def ")
}

// pythonClassMetaclass returns the metaclass keyword argument from the
// tree-sitter `superclasses` argument list, preserving the full dotted value
// (for example abc.ABCMeta). It returns "" when the class declares no
// metaclass= keyword argument.
func pythonClassMetaclass(node *tree_sitter.Node, source []byte) string {
	superclasses := node.ChildByFieldName("superclasses")
	if superclasses == nil {
		return ""
	}
	cursor := superclasses.Walk()
	defer cursor.Close()
	for _, child := range superclasses.NamedChildren(cursor) {
		child := child
		if child.Kind() != "keyword_argument" {
			continue
		}
		if strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source)) != "metaclass" {
			continue
		}
		return strings.TrimSpace(nodeText(child.ChildByFieldName("value"), source))
	}
	return ""
}

func pythonNormalizedAnnotation(annotation string) string {
	trimmed := strings.TrimSpace(annotation)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func pythonModuleScoped(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "lambda":
			return false
		case "module", "class_definition":
			return true
		}
	}
	return true
}

// sortNamedBucket lives in payload_buckets.go.
