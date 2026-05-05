package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonFunctionSignatureRe = regexp.MustCompile(`(?s)^(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)\s*(?:->\s*([^:]+))?:$`)
var pythonClassHeaderRe = regexp.MustCompile(`(?m)^class\s+[A-Za-z_][A-Za-z0-9_]*\s*\((.*)\)\s*:`)

func (e *Engine) parsePython(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("python")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

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
	scope := options.normalizedVariableScope()
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
			if metaclass := pythonClassMetaclass(node, source); metaclass != "" {
				item["metaclass"] = metaclass
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
				"cyclomatic_complexity": cyclomaticComplexity(node),
			}
			decorators := pythonDecorators(node, source)
			item["decorators"] = decorators
			if rootKinds := pythonDeadCodeRootKinds(decorators); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			if pythonFunctionIsGenerator(node) {
				item["semantic_kind"] = "generator"
			}
			if docstring := pythonDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			if options.IndexSource {
				item["source"] = functionSource
			}
			appendBucket(payload, "functions", item)
			for _, annotation := range pythonTypeAnnotations(node, functionSource, name) {
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
			}
			appendBucket(payload, "function_calls", item)
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
	payload["framework_semantics"] = buildPythonFrameworkSemantics(string(source))
	payload["orm_table_mappings"] = buildPythonORMTableMappings(string(source))

	return payload, nil
}

func (e *Engine) preScanPython(path string) ([]string, error) {
	payload, err := e.parsePython(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
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

func pythonClassMetaclass(node *tree_sitter.Node, source []byte) string {
	classSource := nodeText(node, source)
	matches := pythonClassHeaderRe.FindStringSubmatch(classSource)
	if len(matches) != 2 {
		return ""
	}
	for _, argument := range splitPythonParameters(matches[1]) {
		name, value, ok := strings.Cut(argument, "=")
		if !ok || strings.TrimSpace(name) != "metaclass" {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func pythonTypeAnnotations(node *tree_sitter.Node, functionSource string, functionName string) []map[string]any {
	signature := pythonFunctionSignature(functionSource)
	if signature == "" {
		return nil
	}
	matches := pythonFunctionSignatureRe.FindStringSubmatch(signature)
	if len(matches) != 4 {
		return nil
	}

	lineNumber := nodeLine(node)
	annotations := make([]map[string]any, 0)
	for _, parameter := range splitPythonParameters(matches[2]) {
		name, annotationType, ok := parsePythonParameterAnnotation(parameter)
		if !ok {
			continue
		}
		annotations = append(annotations, map[string]any{
			"name":            name,
			"line_number":     lineNumber,
			"type":            annotationType,
			"annotation_kind": "parameter",
			"context":         functionName,
			"lang":            "python",
		})
	}
	if returnType := strings.TrimSpace(matches[3]); returnType != "" {
		annotations = append(annotations, map[string]any{
			"name":            functionName,
			"line_number":     lineNumber,
			"type":            pythonNormalizedAnnotation(returnType),
			"annotation_kind": "return",
			"context":         functionName,
			"lang":            "python",
		})
	}
	return annotations
}

func pythonFunctionSignature(functionSource string) string {
	trimmed := strings.TrimSpace(functionSource)
	if trimmed == "" {
		return ""
	}
	if bodyIndex := strings.Index(trimmed, ":\n"); bodyIndex >= 0 {
		return trimmed[:bodyIndex+1]
	}
	if colonIndex := strings.Index(trimmed, ":"); colonIndex >= 0 {
		return trimmed[:colonIndex+1]
	}
	return trimmed
}

func splitPythonParameters(parameters string) []string {
	parts := make([]string, 0)
	var current strings.Builder
	depth := 0
	for _, char := range parameters {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
		}
		current.WriteRune(char)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func parsePythonParameterAnnotation(parameter string) (string, string, bool) {
	trimmed := strings.TrimSpace(parameter)
	if trimmed == "" || trimmed == "/" || trimmed == "*" {
		return "", "", false
	}
	colonIndex := strings.Index(trimmed, ":")
	if colonIndex < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(trimmed[:colonIndex])
	if name == "" {
		return "", "", false
	}
	name = strings.TrimPrefix(name, "**")
	name = strings.TrimPrefix(name, "*")
	annotation := strings.TrimSpace(trimmed[colonIndex+1:])
	if equalsIndex := strings.Index(annotation, "="); equalsIndex >= 0 {
		annotation = strings.TrimSpace(annotation[:equalsIndex])
	}
	annotation = pythonNormalizedAnnotation(annotation)
	if annotation == "" {
		return "", "", false
	}
	return name, annotation, true
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

func convertNotebookToTempPython(path string, source []byte) (string, error) {
	code, err := pythonNotebookSource(source)
	if err != nil {
		return "", fmt.Errorf("convert notebook %q: %w", path, err)
	}

	tempFile, err := os.CreateTemp("", "eshu-notebook-*.py")
	if err != nil {
		return "", fmt.Errorf("create temporary python file for %q: %w", path, err)
	}
	defer func() {
		_ = tempFile.Close()
	}()

	if _, err := tempFile.WriteString(code); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", fmt.Errorf("write temporary python file for %q: %w", path, err)
	}
	return tempFile.Name(), nil
}

func pythonNotebookSource(source []byte) (string, error) {
	var notebook map[string]any
	if err := json.Unmarshal(source, &notebook); err != nil {
		return "", fmt.Errorf("decode notebook json: %w", err)
	}

	cells, _ := notebook["cells"].([]any)
	if len(cells) == 0 {
		return "", nil
	}

	codeCells := make([]string, 0, len(cells))
	for _, rawCell := range cells {
		cell, ok := rawCell.(map[string]any)
		if !ok {
			continue
		}
		if !strings.EqualFold(fmt.Sprint(cell["cell_type"]), "code") {
			continue
		}
		cellSource := notebookCellSource(cell["source"])
		if strings.TrimSpace(cellSource) == "" {
			continue
		}
		codeCells = append(codeCells, cellSource)
	}
	return strings.Join(codeCells, "\n\n"), nil
}

func notebookCellSource(raw any) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "")
	case []string:
		return strings.Join(typed, "")
	default:
		return fmt.Sprint(raw)
	}
}

func sortNamedBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	slices.SortFunc(items, func(left, right map[string]any) int {
		leftLine, _ := left["line_number"].(int)
		rightLine, _ := right["line_number"].(int)
		if leftLine != rightLine {
			return leftLine - rightLine
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
	payload[key] = items
}

func collectBucketNames(payload map[string]any, keys ...string) []string {
	var names []string
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) != "" {
				names = append(names, filepath.Clean(name))
			}
		}
	}
	return names
}
