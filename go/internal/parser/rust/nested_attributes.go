package rust

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func appendRustNestedAttribute(payload map[string]any, node *tree_sitter.Node, source []byte, targetKind string) {
	attributes := rustLeadingAttributeTexts(shared.NodeText(node, source))
	if len(attributes) == 0 {
		return
	}
	appendRustNestedAttributeItem(payload, node, source, targetKind, attributes)
}

func appendRustNestedAttributeFromAttribute(payload map[string]any, node *tree_sitter.Node, source []byte) {
	parent := node.Parent()
	if parent == nil || (parent.Kind() != "field_declaration_list" && parent.Kind() != "enum_variant_list") {
		return
	}
	if previous := node.PrevNamedSibling(); previous != nil && previous.Kind() == "attribute_item" {
		return
	}
	attributes := make([]string, 0, 1)
	for current := node; current != nil && current.Kind() == "attribute_item"; current = current.NextNamedSibling() {
		attribute := strings.TrimSpace(shared.NodeText(current, source))
		if attribute != "" {
			attributes = appendUniqueString(attributes, attribute)
		}
	}
	target := node.NextNamedSibling()
	for target != nil && target.Kind() == "attribute_item" {
		target = target.NextNamedSibling()
	}
	if target == nil {
		return
	}
	switch target.Kind() {
	case "field_declaration":
		appendRustNestedAttributeItem(payload, target, source, "field", attributes)
	case "enum_variant":
		appendRustNestedAttributeItem(payload, target, source, "enum_variant", attributes)
	}
}

func appendRustNestedAttributeItem(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	targetKind string,
	attributes []string,
) {
	ownerKind, ownerName := rustAttributeOwner(node, source)
	targetName := rustNestedAttributeTargetName(node, source, targetKind)
	if ownerName == "" || targetName == "" || len(attributes) == 0 {
		return
	}
	item := map[string]any{
		"name":        ownerName + "." + targetName,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"owner":       ownerName,
		"owner_kind":  ownerKind,
		"target":      targetName,
		"target_kind": targetKind,
		"lang":        "rust",
	}
	rustApplyAttributeMetadata(item, attributes)
	shared.AppendBucket(payload, "annotations", item)
}

func rustAttributeOwner(node *tree_sitter.Node, source []byte) (string, string) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "struct_item":
			return "struct", rustOwnerTypeName(current, source)
		case "enum_item":
			return "enum", rustOwnerTypeName(current, source)
		}
	}
	return "", ""
}

func rustOwnerTypeName(node *tree_sitter.Node, source []byte) string {
	nameNode := firstNamedDescendant(node, "type_identifier")
	return strings.TrimSpace(shared.NodeText(nameNode, source))
}

func rustNestedAttributeTargetName(node *tree_sitter.Node, source []byte, targetKind string) string {
	switch targetKind {
	case "field":
		return strings.TrimSpace(shared.NodeText(firstNamedDescendant(node, "field_identifier", "identifier"), source))
	case "enum_variant":
		return strings.TrimSpace(shared.NodeText(firstNamedDescendant(node, "identifier"), source))
	default:
		return ""
	}
}
