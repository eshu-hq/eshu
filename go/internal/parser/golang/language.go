package golang

import (
	"fmt"
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
	payload["embedded_shell_commands"] = embeddedShellCommandPayloads(string(source))
	root := tree.RootNode()
	// Build the parent-lookup once per file so every helper that walks
	// ancestors does so in amortized O(1) per step instead of paying
	// tree-sitter's O(depth) Node.Parent() cost on each call (#161).
	lookup := goBuildParentLookup(root)
	importAliases := goImportAliasIndex(root, source)
	constructorReturns := goConstructorReturnTypes(root, source)
	localNameBindings := goLocalNameBindings(root, source, lookup)
	localReceiverBindings := goLocalReceiverBindings(root, source, constructorReturns, lookup)
	awsSDKServiceBindings := goAWSSDKReceiverBindings(root, source, goAWSSDKServiceAliases(importAliases), lookup)
	deadCodeEvidence := goDeadCodeEvidence(
		root,
		source,
		importAliases,
		options.GoImportedInterfaceParamMethods,
		options.GoDirectMethodCallRoots,
		options.GoPackageImportPath,
		localNameBindings,
		lookup,
	)
	scope := options.NormalizedVariableScope()
	packageImportPath := strings.TrimSpace(options.GoPackageImportPath)

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
				"parameter_count":       goParameterCount(node.ChildByFieldName("parameters"), source),
				"cyclomatic_complexity": cyclomaticComplexity(node, source),
			}
			if docstring := goDocstring(node, source); docstring != "" {
				item["docstring"] = docstring
			}
			classContext := goReceiverContext(node, source)
			if classContext != "" {
				item["class_context"] = classContext
			}
			if packageImportPath != "" {
				item["package_import_path"] = packageImportPath
				if scipSymbol := goSCIPSymbol(packageImportPath, classContext, name); scipSymbol != "" {
					item["scip_symbol"] = scipSymbol
				}
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
			item := map[string]any{
				"name":        name,
				"line_number": nodeLine(pathNode),
				"lang":        "go",
			}
			if aliasNode := node.ChildByFieldName("name"); aliasNode != nil {
				if alias := strings.TrimSpace(nodeText(aliasNode, source)); alias != "" && alias != "." && alias != "_" {
					item["alias"] = alias
				}
			}
			shared.AppendBucket(payload, "imports", item)
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
			goAnnotateCallMetadata(
				item,
				node,
				functionNode,
				source,
				importAliases,
				localNameBindings,
				localReceiverBindings,
				awsSDKServiceBindings,
				lookup,
			)
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
			if scope == "module" && goInsideFunction(node, lookup) {
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
	for _, item := range goFunctionValueReferenceCalls(root, source, localNameBindings, lookup) {
		shared.AppendBucket(payload, "function_calls", item)
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "structs")
	shared.SortNamedBucket(payload, "interfaces")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")

	// Dataflow and taint facts are opt-in: when off the payload is byte-identical
	// to before this feature because no key is added.
	if options.EmitDataflow {
		dataflow, findings := goEmitDataflowBuckets(root, source)
		if len(dataflow) > 0 {
			payload["dataflow_functions"] = dataflow
		}
		if len(findings) > 0 {
			payload["taint_findings"] = findings
		}
		interprocRows, summaryRows, sourceRows := goInterprocPayloads(root, source, options.RepositoryID, options.GoPackageImportPath)
		if len(interprocRows) > 0 {
			payload["interproc_findings"] = interprocRows
		}
		if len(summaryRows) > 0 {
			payload["dataflow_summaries"] = summaryRows
		}
		if len(sourceRows) > 0 {
			payload["dataflow_sources"] = sourceRows
		}
	}

	return payload, nil
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

func embeddedShellCommandPayloads(source string) []map[string]any {
	commands := EmbeddedShellCommands(source)
	if len(commands) == 0 {
		return []map[string]any{}
	}
	payload := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		payload = append(payload, map[string]any{
			"function_name":        command.FunctionName,
			"function_line_number": command.FunctionLineNumber,
			"line_number":          command.LineNumber,
			"api":                  command.API,
			"language":             command.Language,
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
	localNameBindings []goLocalNameBinding,
	localReceiverBindings []goLocalReceiverBinding,
	awsSDKServiceBindings []goLocalReceiverBinding,
	lookup *goParentLookup,
) {
	receiverIdentifier, receiverIsImportAlias := goCallReceiverIdentifier(functionNode, source, importAliases)
	if receiverIdentifier == "" {
		goAnnotateCallChainMetadata(item, callNode, functionNode, source, localReceiverBindings)
		return
	}
	if receiverIsImportAlias && goNameIsLocallyBound(receiverIdentifier, nodeLine(callNode), localNameBindings) {
		receiverIsImportAlias = false
	}

	item["receiver_identifier"] = receiverIdentifier
	item["receiver_is_import_alias"] = receiverIsImportAlias
	if receiverIsImportAlias {
		if importPath := goImportPathForAlias(receiverIdentifier, importAliases); importPath != "" {
			name, _ := item["name"].(string)
			if stableSymbol := goSCIPSymbol(importPath, "", name); stableSymbol != "" {
				item["stable_symbol_key"] = stableSymbol
			}
		}
	}
	if !receiverIsImportAlias {
		if receiverType := goInferredReceiverType(receiverIdentifier, nodeLine(callNode), localReceiverBindings); receiverType != "" {
			item["inferred_obj_type"] = receiverType
		}
		if service := goInferredReceiverSDKService(receiverIdentifier, nodeLine(callNode), awsSDKServiceBindings); service != "" {
			item["receiver_sdk_service"] = service
		}
	}

	enclosingReceiverName, enclosingClassContext := goEnclosingMethodReceiver(callNode, source, lookup)
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

func goEnclosingMethodReceiver(callNode *tree_sitter.Node, source []byte, lookup *goParentLookup) (string, string) {
	for current := callNode; current != nil; current = lookup.Parent(current) {
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
