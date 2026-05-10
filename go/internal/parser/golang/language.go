package golang

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Go parser payloads from one source file using the provided
// tree-sitter parser.
func Parse(
	parser *tree_sitter.Parser,
	path string,
	isDependency bool,
	options shared.Options,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "go", isDependency)
	payload["interfaces"] = []map[string]any{}
	payload["structs"] = []map[string]any{}
	payload["embedded_sql_queries"] = embeddedSQLQueryPayloads(string(source))
	root := tree.RootNode()
	importAliases := goImportAliasIndex(root, source)
	constructorReturns := goConstructorReturnTypes(root, source)
	localReceiverBindings := goLocalReceiverBindings(root, source, constructorReturns)
	deadCodeEvidence := goDeadCodeEvidence(root, source, importAliases, options.GoImportedInterfaceParamMethods)
	scope := options.NormalizedVariableScope()

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
			nameNode := node.ChildByFieldName("name")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":                  name,
				"line_number":           nodeLine(nameNode),
				"end_line":              nodeEndLine(node),
				"decorators":            []string{},
				"lang":                  "go",
				"cyclomatic_complexity": cyclomaticComplexity(node),
			}
			if docstring := goDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			if classContext := goReceiverContext(node, source); classContext != "" {
				item["class_context"] = classContext
			}
			if returnType := goTypeNameFromNode(node.ChildByFieldName("result"), source); returnType != "" {
				item["return_type"] = returnType
			}
			if rootKinds := goDeadCodeRootKinds(node, source, importAliases, deadCodeEvidence.functionRootKinds); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			if options.IndexSource {
				item["source"] = nodeText(node, source)
			}
			shared.AppendBucket(payload, "functions", item)
		case "type_spec":
			nameNode := node.ChildByFieldName("name")
			typeNode := node.ChildByFieldName("type")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" || typeNode == nil {
				return
			}
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        "go",
			}
			if docstring := goDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			switch typeNode.Kind() {
			case "struct_type":
				if rootKinds := deadCodeEvidence.structRootKinds[strings.ToLower(name)]; len(rootKinds) > 0 {
					item["dead_code_root_kinds"] = rootKinds
				}
				shared.AppendBucket(payload, "structs", item)
			case "interface_type":
				if rootKinds := deadCodeEvidence.interfaceRootKinds[strings.ToLower(name)]; len(rootKinds) > 0 {
					item["dead_code_root_kinds"] = rootKinds
				}
				shared.AppendBucket(payload, "interfaces", item)
			}
		case "import_spec":
			pathNode := node.ChildByFieldName("path")
			if pathNode == nil {
				return
			}
			name := strings.Trim(nodeText(pathNode, source), `"`)
			if strings.TrimSpace(name) == "" {
				return
			}
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        name,
				"line_number": nodeLine(pathNode),
				"lang":        "go",
			})
		case "call_expression":
			functionNode := node.ChildByFieldName("function")
			name := goCallName(functionNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			item := map[string]any{
				"name":        name,
				"full_name":   strings.TrimSpace(nodeText(functionNode, source)),
				"line_number": nodeLine(node),
				"lang":        "go",
			}
			goAnnotateCallMetadata(item, node, functionNode, source, importAliases, localReceiverBindings)
			shared.AppendBucket(payload, "function_calls", item)
		case "composite_literal":
			name := goCompositeLiteralTypeName(node.ChildByFieldName("type"), source)
			if strings.TrimSpace(name) == "" {
				return
			}
			shared.AppendBucket(payload, "function_calls", map[string]any{
				"name":        name,
				"full_name":   strings.TrimSpace(nodeText(node.ChildByFieldName("type"), source)),
				"line_number": nodeLine(node),
				"call_kind":   "go.composite_literal_type_reference",
				"lang":        "go",
			})
		case "var_spec", "const_spec":
			if scope == "module" && goInsideFunction(node) {
				return
			}
			for _, item := range goVariableNames(node, source) {
				shared.AppendBucket(payload, "variables", item)
			}
		case "short_var_declaration":
			if scope == "module" {
				return
			}
			for _, item := range goShortVariableNames(node, source) {
				shared.AppendBucket(payload, "variables", item)
			}
		}
	})
	for _, item := range goFunctionValueReferenceCalls(root, source) {
		shared.AppendBucket(payload, "function_calls", item)
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "structs")
	shared.SortNamedBucket(payload, "interfaces")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")

	return payload, nil
}

// PreScan returns deterministic Go symbols used by collector import-map
// prescans.
func PreScan(parser *tree_sitter.Parser, path string) ([]string, error) {
	payload, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "structs", "interfaces")
	slices.Sort(names)
	return names, nil
}

func embeddedSQLQueryPayloads(source string) []map[string]any {
	queries := EmbeddedSQLQueries(source)
	if len(queries) == 0 {
		return []map[string]any{}
	}
	payload := make([]map[string]any, 0, len(queries))
	for _, query := range queries {
		payload = append(payload, map[string]any{
			"function_name":        query.FunctionName,
			"function_line_number": query.FunctionLineNumber,
			"table_name":           query.TableName,
			"operation":            query.Operation,
			"line_number":          query.LineNumber,
			"api":                  query.API,
		})
	}
	return payload
}

func goCallName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return nodeText(node, source)
	case "selector_expression":
		field := node.ChildByFieldName("field")
		return nodeText(field, source)
	default:
		return ""
	}
}

func goCompositeLiteralTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "type_identifier" {
		return nodeText(node, source)
	}
	nameNode := firstNamedDescendant(node, "type_identifier")
	return nodeText(nameNode, source)
}

func goAnnotateCallMetadata(
	item map[string]any,
	callNode *tree_sitter.Node,
	functionNode *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	localReceiverBindings []goLocalReceiverBinding,
) {
	receiverIdentifier, receiverIsImportAlias := goCallReceiverIdentifier(functionNode, source, importAliases)
	if receiverIdentifier == "" {
		return
	}

	item["receiver_identifier"] = receiverIdentifier
	item["receiver_is_import_alias"] = receiverIsImportAlias
	if !receiverIsImportAlias {
		if receiverType := goInferredReceiverType(receiverIdentifier, nodeLine(callNode), localReceiverBindings); receiverType != "" {
			item["inferred_obj_type"] = receiverType
		}
	}

	enclosingReceiverName, enclosingClassContext := goEnclosingMethodReceiver(callNode, source)
	if receiverIsImportAlias || enclosingReceiverName == "" || enclosingClassContext == "" {
		return
	}
	if receiverIdentifier == enclosingReceiverName {
		item["class_context"] = enclosingClassContext
	}
}

func goCallReceiverIdentifier(
	functionNode *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) (string, bool) {
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return "", false
	}

	baseNode := functionNode.ChildByFieldName("operand")
	if baseNode == nil {
		cursor := functionNode.Walk()
		defer cursor.Close()
		children := functionNode.NamedChildren(cursor)
		if len(children) == 0 {
			return "", false
		}
		baseNode = &children[0]
	}
	if baseNode.Kind() != "identifier" {
		return "", false
	}

	receiverIdentifier := strings.TrimSpace(nodeText(baseNode, source))
	if receiverIdentifier == "" {
		return "", false
	}
	return receiverIdentifier, goIdentifierMatchesImportAlias(receiverIdentifier, importAliases)
}

func goIdentifierMatchesImportAlias(identifier string, importAliases map[string][]string) bool {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return false
	}
	for _, aliases := range importAliases {
		for _, alias := range aliases {
			if alias == trimmed {
				return true
			}
		}
	}
	return false
}

func goEnclosingMethodReceiver(callNode *tree_sitter.Node, source []byte) (string, string) {
	for current := callNode; current != nil; current = current.Parent() {
		if current.Kind() != "method_declaration" {
			continue
		}
		return goMethodReceiverBinding(current, source)
	}
	return "", ""
}

func goMethodReceiverBinding(node *tree_sitter.Node, source []byte) (string, string) {
	if node == nil {
		return "", ""
	}

	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return "", ""
	}

	cursor := receiver.Walk()
	defer cursor.Close()
	for _, child := range receiver.NamedChildren(cursor) {
		child := child
		if child.Kind() != "parameter_declaration" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		receiverName := strings.TrimSpace(nodeText(nameNode, source))
		receiverType := goReceiverContext(node, source)
		if receiverName != "" || receiverType != "" {
			return receiverName, receiverType
		}
	}

	receiverType := goReceiverContext(node, source)
	if receiverType == "" {
		return "", ""
	}
	nameNode := firstNamedDescendant(receiver, "identifier")
	return strings.TrimSpace(nodeText(nameNode, source)), receiverType
}

func goInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			return true
		}
	}
	return false
}

func goVariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return []map[string]any{{
		"name":        nodeText(nameNode, source),
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        "go",
	}}
}

func goShortVariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
	left := node.ChildByFieldName("left")
	if left == nil {
		return nil
	}

	var items []map[string]any
	cursor := left.Walk()
	defer cursor.Close()
	for _, child := range left.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" {
			continue
		}
		items = append(items, map[string]any{
			"name":        nodeText(&child, source),
			"line_number": nodeLine(&child),
			"end_line":    nodeEndLine(node),
			"lang":        "go",
		})
	}
	return items
}

func goDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	startLine := nodeLine(node) - 2
	if startLine < 0 || startLine >= len(lines) {
		return ""
	}

	comments := make([]string, 0)
	for index := startLine; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			if len(comments) == 0 {
				return ""
			}
			break
		}
		if strings.HasPrefix(trimmed, "//") {
			comments = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))}, comments...)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") && strings.HasSuffix(trimmed, "*/") {
			comments = append([]string{strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "/*"), "*/"))}, comments...)
			continue
		}
		break
	}

	return strings.TrimSpace(strings.Join(comments, "\n"))
}

func goReceiverContext(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}

	typeNode := firstNamedDescendant(receiver,
		"type_identifier",
		"qualified_type",
		"generic_type",
		"pointer_type",
		"array_type",
		"slice_type",
	)
	if typeNode == nil {
		return ""
	}

	value := strings.TrimSpace(nodeText(typeNode, source))
	value = strings.TrimSpace(strings.TrimPrefix(value, "*"))
	value = strings.Trim(value, "[]")
	if index := strings.LastIndex(value, "."); index >= 0 {
		value = value[index+1:]
	}
	return strings.TrimSpace(value)
}
