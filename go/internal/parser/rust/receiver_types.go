// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func rustCallReceiverType(node *tree_sitter.Node, source []byte) string {
	fullName := rustCallFullName(node, source)
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 {
		return ""
	}
	receiver := strings.TrimSpace(fullName[:dot])
	if receiver == "" || strings.ContainsAny(receiver, ".():[]{}") || strings.Contains(receiver, "::") {
		return ""
	}
	parameterTypes := rustNearestFunctionParameterTypes(node, source)
	return parameterTypes[receiver]
}

func rustNearestFunctionParameterTypes(node *tree_sitter.Node, source []byte) map[string]string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_item", "function_signature_item":
			signature := rustSignatureHeader(shared.NodeText(current, source))
			return rustFunctionParameterTypes(signature)
		}
	}
	return nil
}

func rustFunctionParameterTypes(signature string) map[string]string {
	open := strings.Index(signature, "(")
	if open < 0 {
		return nil
	}
	close := rustMatchingParenIndex(signature, open)
	if close <= open {
		return nil
	}
	out := make(map[string]string)
	for _, parameter := range rustSplitTopLevel(signature[open+1:close], ',') {
		name, typ, ok := rustParameterNameAndType(parameter)
		if !ok {
			continue
		}
		out[name] = typ
	}
	return out
}

func rustMatchingParenIndex(text string, open int) int {
	depth := 0
	for idx, r := range text[open:] {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return open + idx
			}
		}
	}
	return -1
}

func rustParameterNameAndType(parameter string) (string, string, bool) {
	trimmed := strings.TrimSpace(parameter)
	if trimmed == "" || trimmed == "self" || strings.HasPrefix(trimmed, "&self") || strings.HasPrefix(trimmed, "&mut self") {
		return "", "", false
	}
	idx := rustParameterColonIndex(trimmed)
	if idx <= 0 || idx >= len(trimmed)-1 {
		return "", "", false
	}
	name := strings.TrimSpace(trimmed[:idx])
	typ := rustNormalizeReceiverType(trimmed[idx+1:])
	if name == "" || typ == "" {
		return "", "", false
	}
	if strings.HasPrefix(name, "mut ") {
		name = strings.TrimSpace(strings.TrimPrefix(name, "mut "))
	}
	if !rustIdentifierPattern.MatchString(name) {
		return "", "", false
	}
	return name, typ, true
}

func rustParameterColonIndex(parameter string) int {
	for idx, r := range parameter {
		if r != ':' {
			continue
		}
		if idx > 0 && parameter[idx-1] == ':' {
			continue
		}
		if idx+1 < len(parameter) && parameter[idx+1] == ':' {
			continue
		}
		return idx
	}
	return -1
}

func rustNormalizeReceiverType(typ string) string {
	trimmed := strings.TrimSpace(typ)
	for strings.HasPrefix(trimmed, "&") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "&"))
		if strings.HasPrefix(trimmed, "'") {
			fields := strings.Fields(trimmed)
			if len(fields) > 1 {
				trimmed = strings.Join(fields[1:], " ")
			}
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "mut "))
	}
	return rustBaseTypeName(trimmed)
}
