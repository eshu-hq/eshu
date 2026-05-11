package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goLocalMapValueTypeBinding struct {
	variable   string
	typeName   string
	line       int
	scopeStart int
	scopeEnd   int
}

// goLocalMapValueTypes records lexical map value types so range receiver
// inference follows Go block shadowing instead of package-wide variable names.
// The lookup is threaded so nested scope helpers walk ancestors in O(1) per
// step instead of repeatedly re-entering tree-sitter cgo; see #161.
func goLocalMapValueTypes(root *tree_sitter.Node, source []byte, lookup *goParentLookup) []goLocalMapValueTypeBinding {
	bindings := make([]goLocalMapValueTypeBinding, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
			bindings = append(bindings, goParameterMapValueTypes(node, source)...)
		case "func_literal":
			bindings = append(bindings, goParameterMapValueTypes(node, source)...)
		case "short_var_declaration", "assignment_statement":
			leftNames := goIdentifierNodes(node.ChildByFieldName("left"), source)
			values := goExpressionNodes(node.ChildByFieldName("right"))
			bindings = append(bindings, goAssignedMapValueTypes(node, leftNames, values, source, lookup)...)
		case "var_spec":
			nameNodes := goIdentifierNodes(node.ChildByFieldName("name"), source)
			if typeName := goMapValueTypeNameFromNode(node.ChildByFieldName("type"), source); typeName != "" {
				for _, nameNode := range nameNodes {
					if binding := goNewMapValueTypeBinding(node, nameNode, typeName, source, lookup); binding.variable != "" {
						bindings = append(bindings, binding)
					}
				}
				return
			}
			bindings = append(bindings, goAssignedMapValueTypes(node, nameNodes, goExpressionNodes(node.ChildByFieldName("value")), source, lookup)...)
		}
	})
	return bindings
}

func goParameterMapValueTypes(node *tree_sitter.Node, source []byte) []goLocalMapValueTypeBinding {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return nil
	}
	bindings := make([]goLocalMapValueTypeBinding, 0)
	walkDirectNamed(parameters, func(param *tree_sitter.Node) {
		if param.Kind() != "parameter_declaration" {
			return
		}
		typeName := goMapValueTypeNameFromNode(param.ChildByFieldName("type"), source)
		if typeName == "" {
			return
		}
		for _, nameNode := range goIdentifierNodes(param.ChildByFieldName("name"), source) {
			name := strings.TrimSpace(nodeText(nameNode, source))
			if name != "" {
				bindings = append(bindings, goLocalMapValueTypeBinding{
					variable:   name,
					typeName:   typeName,
					line:       nodeLine(node),
					scopeStart: nodeLine(body),
					scopeEnd:   nodeEndLine(body),
				})
			}
		}
	})
	return bindings
}

func goAssignedMapValueTypes(
	node *tree_sitter.Node,
	nameNodes []*tree_sitter.Node,
	valueNodes []*tree_sitter.Node,
	source []byte,
	lookup *goParentLookup,
) []goLocalMapValueTypeBinding {
	if len(nameNodes) == 0 || len(valueNodes) == 0 {
		return nil
	}
	count := len(nameNodes)
	if len(valueNodes) < count {
		count = len(valueNodes)
	}
	bindings := make([]goLocalMapValueTypeBinding, 0, count)
	for i := 0; i < count; i++ {
		typeName := goMapValueTypeNameFromExpression(valueNodes[i], source)
		if typeName == "" {
			continue
		}
		if binding := goNewMapValueTypeBinding(node, nameNodes[i], typeName, source, lookup); binding.variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func goNewMapValueTypeBinding(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	typeName string,
	source []byte,
	lookup *goParentLookup,
) goLocalMapValueTypeBinding {
	scope := goNearestLexicalScope(node, lookup)
	if scope == nil {
		return goLocalMapValueTypeBinding{}
	}
	return goLocalMapValueTypeBinding{
		variable:   strings.TrimSpace(nodeText(nameNode, source)),
		typeName:   typeName,
		line:       nodeLine(node),
		scopeStart: nodeLine(scope),
		scopeEnd:   nodeEndLine(scope),
	}
}

func goLocalReceiverBindingsFromRangeClause(
	node *tree_sitter.Node,
	source []byte,
	mapValueTypes []goLocalMapValueTypeBinding,
	lookup *goParentLookup,
) []goLocalReceiverBinding {
	rangeNode := node
	if node.Kind() == "for_statement" {
		if child := firstNamedDescendant(node, "range_clause"); child != nil {
			rangeNode = child
		}
	}
	right := goUnwrapSingleExpression(rangeNode.ChildByFieldName("right"))
	valueName := ""
	if right == nil || right.Kind() != "identifier" {
		var rightName string
		valueName, rightName = goRangeValueAndSourceNames(rangeNode, source)
		if rightName == "" {
			return nil
		}
		valueType := goMapValueTypeForName(rightName, nodeLine(rangeNode), mapValueTypes)
		if valueType == "" || valueName == "" {
			return nil
		}
		return goRangeValueReceiverBinding(node, rangeNode, valueName, valueType, lookup)
	}
	valueType := goMapValueTypeForName(strings.TrimSpace(nodeText(right, source)), nodeLine(rangeNode), mapValueTypes)
	if valueType == "" {
		return nil
	}
	leftNames := goIdentifierNodes(rangeNode.ChildByFieldName("left"), source)
	if len(leftNames) >= 2 {
		valueName = strings.TrimSpace(nodeText(leftNames[1], source))
	} else {
		valueName, _ = goRangeValueAndSourceNames(rangeNode, source)
	}
	if valueName == "" {
		return nil
	}
	return goRangeValueReceiverBinding(node, rangeNode, valueName, valueType, lookup)
}

func goMapValueTypeForName(
	variable string,
	rangeLine int,
	bindings []goLocalMapValueTypeBinding,
) string {
	variable = strings.TrimSpace(variable)
	if variable == "" || rangeLine <= 0 {
		return ""
	}
	var best goLocalMapValueTypeBinding
	for _, binding := range bindings {
		if binding.variable != variable ||
			binding.line > rangeLine ||
			rangeLine < binding.scopeStart ||
			rangeLine > binding.scopeEnd {
			continue
		}
		if best.typeName == "" ||
			binding.line > best.line ||
			spanWidthForGoMapBinding(binding) < spanWidthForGoMapBinding(best) {
			best = binding
		}
	}
	return best.typeName
}

func spanWidthForGoMapBinding(binding goLocalMapValueTypeBinding) int {
	return binding.scopeEnd - binding.scopeStart
}

func goRangeValueReceiverBinding(
	scopeNode *tree_sitter.Node,
	rangeNode *tree_sitter.Node,
	valueName string,
	valueType string,
	lookup *goParentLookup,
) []goLocalReceiverBinding {
	scope := goNearestLexicalScope(scopeNode, lookup)
	if scope == nil {
		return nil
	}
	return []goLocalReceiverBinding{{
		variable:   valueName,
		typeName:   valueType,
		line:       nodeLine(rangeNode),
		scopeStart: nodeLine(scope),
		scopeEnd:   nodeEndLine(scope),
	}}
}

func goRangeValueAndSourceNames(node *tree_sitter.Node, source []byte) (string, string) {
	text := strings.TrimSpace(nodeText(node, source))
	if text == "" {
		return "", ""
	}
	if strings.HasPrefix(text, "for ") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "for "))
	}
	parts := strings.SplitN(text, "range", 2)
	if len(parts) != 2 {
		return "", ""
	}
	left := strings.TrimSpace(parts[0])
	if index := strings.LastIndex(left, ":="); index >= 0 {
		left = strings.TrimSpace(left[:index])
	} else if index := strings.LastIndex(left, "="); index >= 0 {
		left = strings.TrimSpace(left[:index])
	}
	rightFields := strings.Fields(strings.TrimSpace(parts[1]))
	if len(rightFields) == 0 {
		return "", ""
	}
	right := strings.Trim(rightFields[0], "{}()")
	leftParts := strings.Split(left, ",")
	if len(leftParts) < 2 {
		return "", right
	}
	value := strings.TrimSpace(leftParts[1])
	if value == "_" {
		return "", right
	}
	return value, right
}

func goMapValueTypeNameFromExpression(node *tree_sitter.Node, source []byte) string {
	node = goUnwrapSingleExpression(node)
	if node == nil {
		return ""
	}
	if node.Kind() != "composite_literal" {
		return ""
	}
	return goMapValueTypeNameFromNode(node.ChildByFieldName("type"), source)
}

func goMapValueTypeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() != "map_type" {
		return ""
	}
	if valueNode := node.ChildByFieldName("value"); valueNode != nil {
		return goTypeNameFromNode(valueNode, source)
	}
	children := make([]*tree_sitter.Node, 0)
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		children = append(children, child)
	})
	for i := len(children) - 1; i >= 0; i-- {
		if typeName := goTypeNameFromNode(children[i], source); typeName != "" {
			return typeName
		}
	}
	return ""
}
