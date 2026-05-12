package cpp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

func appendNamedType(payload map[string]any, bucket string, node *tree_sitter.Node, source []byte, lang string) {
	nameNode := node.ChildByFieldName("name")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	shared.AppendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        lang,
	})
}

func appendMacro(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	shared.AppendBucket(payload, "macros", map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        lang,
	})
}

func nearestNamedAncestor(node *tree_sitter.Node, source []byte, kinds ...string) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		for _, kind := range kinds {
			if current.Kind() != kind {
				continue
			}
			nameNode := current.ChildByFieldName("name")
			return shared.NodeText(nameNode, source)
		}
	}
	return ""
}

func appendCall(payload map[string]any, nameNode *tree_sitter.Node, source []byte, lang string) {
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"lang":        lang,
	})
}

func sortSystemsPayload(payload map[string]any, keys ...string) {
	for _, key := range keys {
		shared.SortNamedBucket(payload, key)
	}
}

func bucketContainsName(payload map[string]any, bucket string, name string) bool {
	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		existing, _ := item["name"].(string)
		if strings.TrimSpace(existing) == name {
			return true
		}
	}
	return false
}

func cTypedefAliasName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)
	if idx := strings.LastIndex(trimmed, "]"); idx >= 0 {
		trimmed = trimmed[:idx+1]
	}
	fields := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r != '_' &&
			(r < 'A' || r > 'Z') &&
			(r < 'a' || r > 'z') &&
			(r < '0' || r > '9')
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func cTypedefUnderlyingType(node *tree_sitter.Node, source []byte) string {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(typeNode, source))
}

func cTypedefUnderlyingTypeFromBlock(block string) string {
	trimmed := strings.TrimSpace(block)
	trimmed = strings.TrimPrefix(trimmed, "typedef")
	if aliasIndex := strings.LastIndex(trimmed, "}"); aliasIndex >= 0 {
		return strings.TrimSpace(trimmed[:aliasIndex+1])
	}
	if semicolonIndex := strings.LastIndex(trimmed, ";"); semicolonIndex >= 0 {
		trimmed = strings.TrimSpace(trimmed[:semicolonIndex])
	}
	parts := strings.Fields(trimmed)
	if len(parts) <= 1 {
		return strings.TrimSpace(trimmed)
	}
	return strings.Join(parts[:len(parts)-1], " ")
}

func cLikeCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" || node.Kind() == "field_identifier" {
		return node
	}
	return firstNamedDescendant(node, "identifier", "field_identifier")
}
