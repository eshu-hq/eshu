package parser

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (e *Engine) parseJava(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("java")
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
		return nil, fmt.Errorf("parse java file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "java", isDependency)
	payload["interfaces"] = []map[string]any{}
	payload["annotations"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	root := tree.RootNode()
	scope := options.normalizedVariableScope()
	callInference := buildJavaCallInferenceIndex(root, source)
	methodReferences := buildJavaMethodReferenceIndex(root, source)

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_declaration", "record_declaration":
			appendNamedType(payload, "classes", node, source, "java")
		case "interface_declaration":
			appendNamedType(payload, "interfaces", node, source, "java")
		case "annotation_type_declaration":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "annotations", map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"kind":        "declaration",
				"lang":        "java",
			})
		case "annotation", "marker_annotation":
			appendJavaAnnotation(payload, node, source)
		case "enum_declaration":
			appendNamedType(payload, "enums", node, source, "java")
		case "method_declaration", "constructor_declaration":
			appendJavaFunction(payload, node, source, options, methodReferences)
		case "field_declaration":
			for _, item := range javaDeclarators(node, node, source, "java") {
				appendBucket(payload, "variables", item)
			}
		case "local_variable_declaration":
			if scope == "module" {
				return
			}
			for _, item := range javaDeclarators(node, node, source, "java") {
				appendBucket(payload, "variables", item)
			}
		case "import_declaration":
			appendJavaImport(payload, node, source)
		case "method_invocation", "method_reference":
			appendJavaCall(payload, node, source, callInference)
		case "object_creation_expression":
			appendJavaCall(payload, node, source, callInference)
		}
	})

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "interfaces")
	sortNamedBucket(payload, "annotations")
	sortNamedBucket(payload, "enums")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

func appendJavaFunction(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	options Options,
	methodReferences *javaMethodReferenceIndex,
) {
	nameNode := node.ChildByFieldName("name")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(nameNode),
		"end_line":        nodeEndLine(node),
		"lang":            "java",
		"parameter_count": javaParameterCount(node),
	}
	if decorators := javaDecorators(node, source, name); len(decorators) > 0 {
		item["decorators"] = decorators
	}
	if parameterTypes := javaParameterTypes(node, source); len(parameterTypes) > 0 {
		item["parameter_types"] = parameterTypes
	}
	if classContext := nearestNamedAncestor(node, source, "class_declaration", "interface_declaration", "record_declaration"); classContext != "" {
		item["class_context"] = classContext
	}
	if rootKinds := javaDeadCodeRootKinds(node, source, name, methodReferences); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if options.IndexSource {
		item["source"] = nodeText(node, source)
	}
	appendBucket(payload, "functions", item)
}

func (e *Engine) preScanJava(path string) ([]string, error) {
	parser, err := e.runtime.Parser("java")
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
		return nil, fmt.Errorf("parse java file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	names := javaPreScanNames(tree.RootNode(), source)
	slices.Sort(names)
	return names, nil
}

func javaPreScanNames(root *tree_sitter.Node, source []byte) []string {
	var names []string
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_declaration", "interface_declaration", "annotation_type_declaration", "enum_declaration", "record_declaration",
			"method_declaration", "constructor_declaration":
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if name != "" {
				names = append(names, name)
			}
		}
	})
	return names
}

func javaDeclarators(
	declarationNode *tree_sitter.Node,
	ownerNode *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	var items []map[string]any
	walkNamed(ownerNode, func(node *tree_sitter.Node) {
		if node == declarationNode || node.Kind() != "variable_declarator" {
			return
		}
		nameNode := node.ChildByFieldName("name")
		name := nodeText(nameNode, source)
		if strings.TrimSpace(name) == "" {
			return
		}
		items = append(items, map[string]any{
			"name":        name,
			"line_number": nodeLine(nameNode),
			"end_line":    nodeEndLine(declarationNode),
			"lang":        lang,
		})
	})
	return items
}

func javaImportName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return nodeText(&child, source)
	}
	return ""
}

func appendJavaImport(payload map[string]any, node *tree_sitter.Node, source []byte) {
	name := strings.TrimSpace(javaImportName(node, source))
	if name == "" {
		return
	}

	raw := strings.TrimSpace(nodeText(node, source))
	importPath := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "import"), ";"))
	importType := "import"
	if strings.HasPrefix(importPath, "static ") {
		importType = "static"
		importPath = strings.TrimSpace(strings.TrimPrefix(importPath, "static "))
	}

	appendBucket(payload, "imports", map[string]any{
		"name":             name,
		"source":           importPath,
		"alias":            javaImportAlias(importPath),
		"full_import_name": raw,
		"import_type":      importType,
		"line_number":      nodeLine(node),
		"lang":             "java",
	})
}

func javaImportAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "."); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+1:])
	}
	return trimmed
}

func appendJavaCall(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	inference *javaCallInferenceIndex,
) {
	if node == nil {
		return
	}

	var nameNode *tree_sitter.Node
	raw := strings.TrimSpace(nodeText(node, source))
	methodReferenceName := ""
	methodReferenceFullName := ""
	switch node.Kind() {
	case "method_invocation":
		nameNode = node.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = javaFirstTypeIdentifier(node)
		}
	case "method_reference":
		methodReferenceName, methodReferenceFullName = javaMethodReferenceName(raw)
	case "object_creation_expression":
		nameNode = node.ChildByFieldName("type")
		if nameNode == nil {
			nameNode = javaFirstTypeIdentifier(node)
		}
	}
	if nameNode == nil && methodReferenceName == "" {
		return
	}

	name := methodReferenceName
	if name == "" {
		name = strings.TrimSpace(nodeText(nameNode, source))
	}
	if name == "" {
		return
	}

	item := map[string]any{
		"name":           name,
		"line_number":    nodeLine(node),
		"lang":           "java",
		"argument_count": javaArgumentCount(node),
	}
	if methodReferenceFullName != "" {
		item["full_name"] = methodReferenceFullName
		item["call_kind"] = "java.method_reference"
		if classContext := nearestNamedAncestor(node, source, "class_declaration"); classContext != "" {
			item["class_context"] = classContext
		}
	} else if fullName := javaCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	if inferredType := javaCallInferredObjectType(node, source, inference); inferredType != "" {
		item["inferred_obj_type"] = inferredType
	}
	if argumentTypes := javaCallArgumentTypes(node, source, inference); len(argumentTypes) > 0 {
		item["argument_types"] = argumentTypes
	}
	appendBucket(payload, "function_calls", item)
}

func javaParameterCount(node *tree_sitter.Node) int {
	parametersNode := node.ChildByFieldName("parameters")
	count := 0
	walkDirectNamed(parametersNode, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "formal_parameter", "spread_parameter":
			count++
		}
	})
	return count
}

func javaCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "method_invocation":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return ""
		}
		name := strings.TrimSpace(nodeText(nameNode, source))
		if name == "" {
			return ""
		}
		if objectNode := node.ChildByFieldName("object"); objectNode != nil {
			object := strings.TrimSpace(nodeText(objectNode, source))
			if object != "" {
				return object + "." + name
			}
		}
		return name
	case "object_creation_expression":
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			return strings.TrimSpace(nodeText(typeNode, source))
		}
		return ""
	default:
		return strings.TrimSpace(nodeText(node, source))
	}
}

func javaMethodReferenceName(raw string) (string, string) {
	receiver, method, ok := strings.Cut(strings.TrimSpace(raw), "::")
	if !ok {
		return "", ""
	}
	receiver = strings.TrimSpace(receiver)
	method = strings.TrimSpace(method)
	if receiver == "" || method == "" || method == "new" {
		return "", ""
	}
	method = javaTrailingIdentifier(method)
	if method == "" {
		return "", ""
	}
	return method, receiver + "." + method
}

func javaTrailingIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	end := len(value)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:end])
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			break
		}
		end -= size
	}
	start := end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:start])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		start -= size
	}
	return value[start:end]
}

func javaFirstTypeIdentifier(node *tree_sitter.Node) *tree_sitter.Node {
	var result *tree_sitter.Node
	walkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		switch child.Kind() {
		case "type_identifier", "identifier", "scoped_type_identifier", "generic_type":
			result = cloneNode(child)
		}
	})
	return result
}

func appendJavaAnnotation(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "identifier", "scoped_identifier", "type_identifier")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, "annotations", map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"kind":        "applied",
		"target_kind": javaAnnotationTargetKind(node),
		"lang":        "java",
	})
}

func javaAnnotationTargetKind(node *tree_sitter.Node) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration", "annotation_type_declaration", "method_declaration", "constructor_declaration", "field_declaration":
			return current.Kind()
		}
	}
	return ""
}
