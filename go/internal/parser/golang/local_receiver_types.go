package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goLocalReceiverBinding struct {
	variable   string
	typeName   string
	line       int
	scopeStart int
	scopeEnd   int
}

func goConstructorReturnTypes(root *tree_sitter.Node, source []byte) map[string]string {
	returns := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		typeName := goTypeNameFromNode(node.ChildByFieldName("result"), source)
		if typeName == "" {
			return
		}
		returns[name] = typeName
	})
	return returns
}

func goLocalReceiverBindings(
	root *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) []goLocalReceiverBinding {
	bindings := make([]goLocalReceiverBinding, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
			bindings = append(bindings, goLocalReceiverBindingsFromParameters(node, source)...)
		case "short_var_declaration", "assignment_statement":
			bindings = append(bindings, goLocalReceiverBindingsFromAssignment(node, source, constructorReturns)...)
		case "var_spec":
			bindings = append(bindings, goLocalReceiverBindingsFromVarSpec(node, source, constructorReturns)...)
		}
	})
	return bindings
}

func goLocalReceiverBindingsFromParameters(node *tree_sitter.Node, source []byte) []goLocalReceiverBinding {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return nil
	}
	bindings := make([]goLocalReceiverBinding, 0)
	walkDirectNamed(parameters, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter_declaration" {
			return
		}
		typeName := goTypeNameFromNode(child.ChildByFieldName("type"), source)
		if typeName == "" {
			return
		}
		for _, nameNode := range goIdentifierNodes(child.ChildByFieldName("name"), source) {
			variable := strings.TrimSpace(nodeText(nameNode, source))
			if variable == "" {
				continue
			}
			bindings = append(bindings, goLocalReceiverBinding{
				variable:   variable,
				typeName:   typeName,
				line:       nodeLine(node),
				scopeStart: nodeLine(body),
				scopeEnd:   nodeEndLine(body),
			})
		}
	})
	return bindings
}

func goLocalReceiverBindingsFromAssignment(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) []goLocalReceiverBinding {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	names := goIdentifierNodes(left, source)
	values := goExpressionNodes(right)
	if len(names) == 0 || len(values) == 0 {
		return nil
	}
	count := len(names)
	if len(values) < count {
		count = len(values)
	}
	bindings := make([]goLocalReceiverBinding, 0, count)
	for i := 0; i < count; i++ {
		typeName := goConstructorTypeFromExpression(values[i], source, constructorReturns)
		if typeName == "" {
			continue
		}
		if binding := goNewLocalReceiverBinding(node, names[i], typeName, source); binding.variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func goLocalReceiverBindingsFromVarSpec(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) []goLocalReceiverBinding {
	nameNodes := goIdentifierNodes(node.ChildByFieldName("name"), source)
	valueNodes := goExpressionNodes(node.ChildByFieldName("value"))
	if len(nameNodes) == 0 || len(valueNodes) == 0 {
		return nil
	}
	count := len(nameNodes)
	if len(valueNodes) < count {
		count = len(valueNodes)
	}
	bindings := make([]goLocalReceiverBinding, 0, count)
	for i := 0; i < count; i++ {
		typeName := goConstructorTypeFromExpression(valueNodes[i], source, constructorReturns)
		if typeName == "" {
			continue
		}
		if binding := goNewLocalReceiverBinding(node, nameNodes[i], typeName, source); binding.variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func goNewLocalReceiverBinding(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	typeName string,
	source []byte,
) goLocalReceiverBinding {
	scope := goEnclosingFunctionScope(node)
	if scope == nil {
		return goLocalReceiverBinding{}
	}
	return goLocalReceiverBinding{
		variable:   strings.TrimSpace(nodeText(nameNode, source)),
		typeName:   typeName,
		line:       nodeLine(node),
		scopeStart: nodeLine(scope),
		scopeEnd:   nodeEndLine(scope),
	}
}

func goInferredReceiverType(
	receiver string,
	callLine int,
	bindings []goLocalReceiverBinding,
) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" || callLine <= 0 {
		return ""
	}
	var best goLocalReceiverBinding
	for _, binding := range bindings {
		if binding.variable != receiver ||
			binding.line > callLine ||
			callLine < binding.scopeStart ||
			callLine > binding.scopeEnd {
			continue
		}
		if best.typeName == "" || binding.line > best.line || spanWidthForGoBinding(binding) < spanWidthForGoBinding(best) {
			best = binding
		}
	}
	return best.typeName
}

func spanWidthForGoBinding(binding goLocalReceiverBinding) int {
	return binding.scopeEnd - binding.scopeStart
}

func goConstructorTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) string {
	if node == nil || node.Kind() != "call_expression" {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "identifier" {
		return ""
	}
	return constructorReturns[strings.TrimSpace(nodeText(functionNode, source))]
}

func goTypeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node != nil {
		switch node.Kind() {
		case "type_identifier", "qualified_type", "generic_type":
			return goNormalizeTypeName(nodeText(node, source))
		}
	}
	typeNode := firstNamedDescendant(node,
		"type_identifier",
		"qualified_type",
		"generic_type",
		"pointer_type",
		"array_type",
		"slice_type",
	)
	return goNormalizeTypeName(nodeText(typeNode, source))
}

func goNormalizeTypeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(strings.TrimPrefix(value, "*"))
	value = strings.Trim(value, "[]")
	if index := strings.LastIndex(value, "."); index >= 0 {
		value = value[index+1:]
	}
	return strings.TrimSpace(value)
}

func goIdentifierNodes(node *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" && strings.TrimSpace(nodeText(node, source)) != "_" {
		return []*tree_sitter.Node{node}
	}
	nodes := make([]*tree_sitter.Node, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" || strings.TrimSpace(nodeText(&child, source)) == "_" {
			continue
		}
		nodes = append(nodes, &child)
	}
	return nodes
}

func goExpressionNodes(node *tree_sitter.Node) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() != "expression_list" && node.Kind() != "parameter_list" {
		return []*tree_sitter.Node{node}
	}
	nodes := make([]*tree_sitter.Node, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		nodes = append(nodes, &child)
	}
	return nodes
}

func goEnclosingFunctionScope(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			return current
		}
	}
	return nil
}
