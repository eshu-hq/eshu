// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// StructFieldConcreteTypes maps each lower-case struct type name declared in
// root to its field names and their concrete (struct-typed) field types,
// restricted to field types present in structTypes.
func StructFieldConcreteTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) map[string]map[string]string {
	fieldTypes := make(map[string]map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		structName := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if structName == "" || typeNode == nil || typeNode.Kind() != "struct_type" {
			return
		}
		shared.WalkNamed(typeNode, func(child *tree_sitter.Node) {
			if child.Kind() != "field_declaration" {
				return
			}
			concreteType := ConcreteTypeFromTypeNode(child.ChildByFieldName("type"), source, structTypes)
			if concreteType == "" {
				return
			}
			for _, fieldName := range IdentifierNames(child.ChildByFieldName("name"), source) {
				if fieldTypes[structName] == nil {
					fieldTypes[structName] = make(map[string]string)
				}
				fieldTypes[structName][fieldName] = concreteType
			}
		})
	})
	return fieldTypes
}
