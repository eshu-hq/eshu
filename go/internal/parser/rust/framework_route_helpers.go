// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func rustExactStringLiteral(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		return trimmed[1 : len(trimmed)-1], true
	}
	if !strings.HasPrefix(trimmed, "r") {
		return "", false
	}
	hashes := 0
	for idx := 1; idx < len(trimmed) && trimmed[idx] == '#'; idx++ {
		hashes++
	}
	prefix := "r" + strings.Repeat("#", hashes) + "\""
	suffix := "\"" + strings.Repeat("#", hashes)
	if strings.HasPrefix(trimmed, prefix) && strings.HasSuffix(trimmed, suffix) && len(trimmed) >= len(prefix)+len(suffix) {
		return trimmed[len(prefix) : len(trimmed)-len(suffix)], true
	}
	return "", false
}

func rustCallArgumentBody(text string, open int) (string, bool) {
	if open < 0 || open >= len(text) || text[open] != '(' {
		return "", false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for idx := open; idx < len(text); idx++ {
		ch := text[idx]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[open+1 : idx], true
			}
		}
	}
	return "", false
}

func rustNodeHasCfgAncestor(node *tree_sitter.Node, source []byte) bool {
	for current := node; current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_item", "function_signature_item", "impl_item", "mod_item":
			for _, attribute := range rustLeadingAttributes(current, source) {
				path := rustAttributePath(attribute)
				if path == "cfg" || path == "cfg_attr" {
					return true
				}
			}
		}
	}
	return false
}

func rustStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func rustStringSliceValue(value any) []string {
	items, _ := value.([]string)
	return items
}

func rustIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	default:
		return 0
	}
}
