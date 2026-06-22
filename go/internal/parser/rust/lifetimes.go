package rust

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Lifetime collection reads `lifetime` grammar nodes directly instead of
// scanning signature text with a regex. The tree-sitter Rust grammar exposes
// every lifetime as a `lifetime` node (`'a`) under generic parameter lists,
// reference types, type arguments, trait bounds, where clauses, and return
// types, so a node walk reproduces the prior payload at byte-parity: names are
// emitted without the leading apostrophe, first-seen left-to-right order is
// preserved, and duplicates collapse.

// rustLifetimeNameFromNode returns a `lifetime` node's name without the leading
// apostrophe. It returns "" for any other node kind or an empty name.
func rustLifetimeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "lifetime" {
		return ""
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	return strings.TrimSpace(strings.TrimPrefix(text, "'"))
}

// rustCollectLifetimeNames walks a subtree and returns the deduplicated,
// first-seen-ordered lifetime names found under it. Lifetime nodes whose start
// byte falls inside excludeStart..excludeEnd are skipped, which lets callers
// drop an item's body so only the header contributes, matching the previous
// behavior of scanning the source slice that ends at the body's opening brace.
func rustCollectLifetimeNames(node *tree_sitter.Node, source []byte, excludeStart, excludeEnd uint) []string {
	if node == nil {
		return nil
	}
	names := make([]string, 0)
	seen := make(map[string]struct{})
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "lifetime" {
			return
		}
		if excludeEnd > excludeStart {
			start := child.StartByte()
			if start >= excludeStart && start < excludeEnd {
				return
			}
		}
		name := rustLifetimeNameFromNode(child, source)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	})
	if len(names) == 0 {
		return nil
	}
	return names
}

// rustItemBodyRange returns the byte range of an item's body field so header
// scans can exclude lifetimes that live inside a function block, impl/trait
// declaration list, or struct field list.
func rustItemBodyRange(node *tree_sitter.Node) (uint, uint) {
	if node == nil {
		return 0, 0
	}
	if body := node.ChildByFieldName("body"); body != nil {
		return body.StartByte(), body.EndByte()
	}
	return 0, 0
}

// rustSignatureLifetimeNames collects every lifetime in an item's header,
// excluding the body subtree. It replaces a regex scan over the header string.
func rustSignatureLifetimeNames(node *tree_sitter.Node, source []byte) []string {
	bodyStart, bodyEnd := rustItemBodyRange(node)
	return rustCollectLifetimeNames(node, source, bodyStart, bodyEnd)
}

// rustDeclaredLifetimeParameters collects the lifetimes declared in an item's
// own `type_parameters` list (the leading `<...>`), excluding lifetimes that
// only appear in bounds. It replaces the regex scan over the leading angle
// segment used for `lifetime_parameters`.
func rustDeclaredLifetimeParameters(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	typeParams := node.ChildByFieldName("type_parameters")
	if typeParams == nil {
		return nil
	}
	names := make([]string, 0)
	seen := make(map[string]struct{})
	for i := uint(0); i < typeParams.NamedChildCount(); i++ {
		child := typeParams.NamedChild(i)
		if child == nil || child.Kind() != "lifetime_parameter" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		name := rustLifetimeNameFromNode(nameNode, source)
		if name == "" {
			continue
		}
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

// rustReturnLifetimeName returns the first lifetime declared in an item's
// `return_type` field, matching the previous first-after-`->` regex behavior.
func rustReturnLifetimeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	returnType := node.ChildByFieldName("return_type")
	if returnType == nil {
		return ""
	}
	names := rustCollectLifetimeNames(returnType, source, 0, 0)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
