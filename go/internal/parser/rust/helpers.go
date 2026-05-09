package rust

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

func sortSystemsPayload(payload map[string]any, keys ...string) {
	for _, key := range keys {
		shared.SortNamedBucket(payload, key)
	}
}

func rustLeadingLifetimeParameters(signature string) []string {
	trimmed := strings.TrimSpace(signature)
	if !strings.HasPrefix(trimmed, "<") {
		return nil
	}
	segment, ok := rustLeadingAngleSegment(trimmed)
	if !ok {
		return nil
	}
	return rustLifetimeNames(segment)
}

func rustLeadingAngleSegment(text string) (string, bool) {
	if !strings.HasPrefix(text, "<") {
		return "", false
	}
	depth := 0
	for idx, r := range text {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return text[:idx+1], true
			}
		}
	}
	return "", false
}

func rustLifetimeNames(text string) []string {
	matches := rustLifetimePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func rustReturnLifetime(signature string) string {
	idx := strings.Index(signature, "->")
	if idx < 0 {
		return ""
	}
	returnType := strings.TrimSpace(signature[idx+len("->"):])
	lifetimes := rustLifetimeNames(returnType)
	if len(lifetimes) == 0 {
		return ""
	}
	return lifetimes[0]
}

func rustSignatureHeader(text string) string {
	signature := strings.TrimSpace(text)
	if idx := strings.Index(signature, "{"); idx >= 0 {
		signature = signature[:idx]
	}
	return strings.TrimSpace(strings.TrimSuffix(signature, ";"))
}

func rustCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if functionNode := node.ChildByFieldName("function"); functionNode != nil {
		return firstNamedDescendant(functionNode, "identifier", "field_identifier")
	}
	return firstNamedDescendant(node, "identifier", "field_identifier")
}

func appendRustCall(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := rustCallNameNode(node)
	if nameNode == nil {
		return
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"lang":        "rust",
	}
	if fullName := rustCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	shared.AppendBucket(payload, "function_calls", item)
}

func rustCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if functionNode := node.ChildByFieldName("function"); functionNode != nil {
		return strings.TrimSpace(shared.NodeText(functionNode, source))
	}
	if nameNode := firstNamedDescendant(node, "identifier", "field_identifier"); nameNode != nil {
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	return ""
}
