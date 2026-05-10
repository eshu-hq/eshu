package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goStructFieldConcreteTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) map[string]map[string]string {
	fieldTypes := make(map[string]map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		structName := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if structName == "" || typeNode == nil || typeNode.Kind() != "struct_type" {
			return
		}
		walkNamed(typeNode, func(child *tree_sitter.Node) {
			if child.Kind() != "field_declaration" {
				return
			}
			concreteType := goConcreteTypeFromTypeNode(child.ChildByFieldName("type"), source, structTypes)
			if concreteType == "" {
				return
			}
			for _, fieldName := range goIdentifierNames(child.ChildByFieldName("name"), source) {
				if fieldTypes[structName] == nil {
					fieldTypes[structName] = make(map[string]string)
				}
				fieldTypes[structName][fieldName] = concreteType
			}
		})
	})
	return fieldTypes
}
