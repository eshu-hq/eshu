package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goLocalMapValueTypes(root *tree_sitter.Node, source []byte) map[string]string {
	valueTypes := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
			goRecordParameterMapValueTypes(valueTypes, node.ChildByFieldName("parameters"), source)
		case "func_literal":
			goRecordParameterMapValueTypes(valueTypes, node.ChildByFieldName("parameters"), source)
		case "short_var_declaration", "assignment_statement":
			leftNames := goIdentifierNodes(node.ChildByFieldName("left"), source)
			values := goExpressionNodes(node.ChildByFieldName("right"))
			goRecordAssignedMapValueTypes(valueTypes, leftNames, values, source)
		case "var_spec":
			nameNodes := goIdentifierNodes(node.ChildByFieldName("name"), source)
			if typeName := goMapValueTypeNameFromNode(node.ChildByFieldName("type"), source); typeName != "" {
				for _, nameNode := range nameNodes {
					name := strings.TrimSpace(nodeText(nameNode, source))
					if name != "" {
						valueTypes[name] = typeName
					}
				}
				return
			}
			goRecordAssignedMapValueTypes(valueTypes, nameNodes, goExpressionNodes(node.ChildByFieldName("value")), source)
		}
	})
	return valueTypes
}

func goRecordParameterMapValueTypes(target map[string]string, parameters *tree_sitter.Node, source []byte) {
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
				target[name] = typeName
			}
		}
	})
}

func goRecordAssignedMapValueTypes(
	target map[string]string,
	nameNodes []*tree_sitter.Node,
	valueNodes []*tree_sitter.Node,
	source []byte,
) {
	if len(nameNodes) == 0 || len(valueNodes) == 0 {
		return
	}
	count := len(nameNodes)
	if len(valueNodes) < count {
		count = len(valueNodes)
	}
	for i := 0; i < count; i++ {
		typeName := goMapValueTypeNameFromExpression(valueNodes[i], source)
		if typeName == "" {
			continue
		}
		name := strings.TrimSpace(nodeText(nameNodes[i], source))
		if name != "" {
			target[name] = typeName
		}
	}
}

func goLocalReceiverBindingsFromRangeClause(
	node *tree_sitter.Node,
	source []byte,
	mapValueTypes map[string]string,
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
		valueType := mapValueTypes[rightName]
		if valueType == "" || valueName == "" {
			return nil
		}
		return goRangeValueReceiverBinding(node, rangeNode, valueName, valueType)
	}
	valueType := mapValueTypes[strings.TrimSpace(nodeText(right, source))]
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
	return goRangeValueReceiverBinding(node, rangeNode, valueName, valueType)
}

func goRangeValueReceiverBinding(
	scopeNode *tree_sitter.Node,
	rangeNode *tree_sitter.Node,
	valueName string,
	valueType string,
) []goLocalReceiverBinding {
	scope := goNearestLexicalScope(scopeNode)
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
