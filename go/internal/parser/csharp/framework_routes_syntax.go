// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func csharpUsingNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "using_directive" {
			return
		}
		if name := csharpUsingName(node, source); name != "" {
			names[name] = struct{}{}
		}
	})
	return names
}

func csharpHasAnyUsing(usings map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := usings[name]; ok {
			return true
		}
	}
	return false
}

func csharpAttributesFromNode(node *tree_sitter.Node, source []byte) []csharpAttribute {
	attributes := make([]csharpAttribute, 0)
	if node == nil {
		return attributes
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "attribute_list" {
			continue
		}
		attributes = append(attributes, csharpAttributesFromList(&child, source)...)
	}
	return attributes
}

func csharpAttributesFromList(node *tree_sitter.Node, source []byte) []csharpAttribute {
	attributes := make([]csharpAttribute, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "attribute" {
			continue
		}
		name := csharpAttributeNameFromNode(&child, source)
		if name == "" {
			continue
		}
		attributes = append(attributes, csharpAttribute{
			name: name,
			body: csharpAttributeBody(shared.NodeText(&child, source)),
		})
	}
	return attributes
}

func csharpAttributeBody(text string) string {
	open := strings.Index(text, "(")
	if open < 0 {
		return ""
	}
	close := csharpMatchingParen(text, open)
	if close < 0 {
		return ""
	}
	return text[open+1 : close]
}

func csharpSingleStringLiteral(raw string) (string, bool) {
	values := csharpStringLiterals(raw)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func csharpStringLiterals(raw string) []string {
	values := make([]string, 0, 1)
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' || csharpInterpolatedQuote(raw, index) {
			continue
		}
		verbatim := index > 0 && raw[index-1] == '@'
		start := index + 1
		escaped := false
		for index = start; index < len(raw); index++ {
			switch {
			case verbatim && raw[index] == '"' && index+1 < len(raw) && raw[index+1] == '"':
				index++
			case verbatim && raw[index] == '"':
				values = append(values, strings.ReplaceAll(raw[start:index], `""`, `"`))
				goto next
			case !verbatim && escaped:
				escaped = false
			case !verbatim && raw[index] == '\\':
				escaped = true
			case !verbatim && raw[index] == '"':
				values = append(values, raw[start:index])
				goto next
			}
		}
	next:
	}
	return values
}

func csharpInterpolatedQuote(raw string, quoteIndex int) bool {
	for index := quoteIndex - 1; index >= 0; index-- {
		switch raw[index] {
		case '$':
			return true
		case '@':
			continue
		default:
			return false
		}
	}
	return false
}

func csharpHTTPMethodsFromArgument(raw string) []string {
	values := csharpStringLiterals(raw)
	methods := make([]string, 0, len(values))
	for _, value := range values {
		switch method := strings.ToUpper(strings.TrimSpace(value)); method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
			methods = appendCSharpUnique(methods, method)
		}
	}
	return methods
}

func csharpInvocationArguments(raw string) []string {
	open := strings.Index(raw, "(")
	if open < 0 {
		return nil
	}
	close := csharpMatchingParen(raw, open)
	if close < 0 {
		return nil
	}
	return csharpSplitTopLevelArguments(raw[open+1 : close])
}

func csharpSplitTopLevelArguments(raw string) []string {
	args := make([]string, 0)
	start := 0
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	inString := false
	verbatimString := false
	escaped := false
	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if inString {
			switch {
			case verbatimString && ch == '"' && index+1 < len(raw) && raw[index+1] == '"':
				index++
			case verbatimString && ch == '"':
				inString = false
			case !verbatimString && escaped:
				escaped = false
			case !verbatimString && ch == '\\':
				escaped = true
			case !verbatimString && ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
			verbatimString = index > 0 && raw[index-1] == '@'
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				args = append(args, strings.TrimSpace(raw[start:index]))
				start = index + 1
			}
		}
	}
	tail := strings.TrimSpace(raw[start:])
	if tail != "" {
		args = append(args, tail)
	}
	return args
}

func csharpSplitNamedAttributeArgument(raw string) (string, string, bool) {
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	inString := false
	verbatimString := false
	escaped := false
	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if inString {
			switch {
			case verbatimString && ch == '"' && index+1 < len(raw) && raw[index+1] == '"':
				index++
			case verbatimString && ch == '"':
				inString = false
			case !verbatimString && escaped:
				escaped = false
			case !verbatimString && ch == '\\':
				escaped = true
			case !verbatimString && ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
			verbatimString = index > 0 && raw[index-1] == '@'
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '=', ':':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				name := strings.TrimSpace(raw[:index])
				value := strings.TrimSpace(raw[index+1:])
				return name, value, name != "" && value != ""
			}
		}
	}
	return "", "", false
}

func csharpIdentifierArgument(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "=>") || strings.Contains(raw, ".") {
		return ""
	}
	for index, r := range raw {
		if index == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return ""
			}
			continue
		}
		if r != '_' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ""
		}
	}
	return raw
}

func csharpJoinRoutePath(prefix string, path string) string {
	prefix = csharpNormalizeRoutePath(prefix)
	path = csharpNormalizeRoutePath(path)
	if prefix == "" || prefix == "/" {
		return path
	}
	if path == "" || path == "/" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

func csharpNormalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func csharpRoutePathIsExact(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	return !strings.Contains(path, "[") && !strings.Contains(path, "]")
}

func csharpShortAttributeName(name string) string {
	return strings.TrimSuffix(csharpLastTypeSegment(name), "Attribute")
}

func appendCSharpUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func csharpMatchingParen(raw string, open int) int {
	depth := 0
	inString := false
	verbatimString := false
	escaped := false
	for index := open; index < len(raw); index++ {
		ch := raw[index]
		if inString {
			switch {
			case verbatimString && ch == '"' && index+1 < len(raw) && raw[index+1] == '"':
				index++
			case verbatimString && ch == '"':
				inString = false
			case !verbatimString && escaped:
				escaped = false
			case !verbatimString && ch == '\\':
				escaped = true
			case !verbatimString && ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
			verbatimString = index > 0 && raw[index-1] == '@'
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}
