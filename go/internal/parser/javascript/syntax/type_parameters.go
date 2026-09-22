// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

import "strings"

// TypeParameterNames returns the declared type-parameter names from a
// TypeScript generic declaration's source text, in declaration order. It
// looks for the first top-level `<...>` section and takes the leading
// identifier of each comma-separated part (ignoring constraints and
// defaults). An empty slice, never nil, is returned when declaration has no
// type-parameter section.
func TypeParameterNames(declaration string) []string {
	section, ok := delimitedSection(declaration, '<', '>')
	if !ok {
		return []string{}
	}

	parts := splitTopLevelSections(section)
	typeParameters := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		fields := strings.Fields(normalized)
		if len(fields) == 0 {
			continue
		}
		typeParameters = append(typeParameters, fields[0])
	}
	return typeParameters
}

// delimitedSection returns the text strictly between the first top-level
// open/close delimiter pair in text, honoring nesting so an inner `<...>`
// inside a constraint does not close the outer section early. ok is false
// when the delimiters are absent or unbalanced.
func delimitedSection(text string, open, close byte) (string, bool) {
	start := strings.IndexByte(text, open)
	if start < 0 {
		return "", false
	}

	depth := 0
	for index := start; index < len(text); index++ {
		switch text[index] {
		case open:
			depth++
		case close:
			if depth == 0 {
				return "", false
			}
			depth--
			if depth == 0 {
				return text[start+1 : index], true
			}
		}
	}
	return "", false
}

// splitTopLevelSections splits text on commas that are not nested inside
// angle brackets, parens, braces, or square brackets, so a generic
// constraint's own commas (e.g. `T extends Record<string, number>`) do not
// fragment the enclosing type-parameter list.
func splitTopLevelSections(text string) []string {
	sections := make([]string, 0)
	start := 0
	depthAngles := 0
	depthParens := 0
	depthBraces := 0
	depthBrackets := 0

	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '<':
			depthAngles++
		case '>':
			if depthAngles > 0 {
				depthAngles--
			}
		case '(':
			depthParens++
		case ')':
			if depthParens > 0 {
				depthParens--
			}
		case '{':
			depthBraces++
		case '}':
			if depthBraces > 0 {
				depthBraces--
			}
		case '[':
			depthBrackets++
		case ']':
			if depthBrackets > 0 {
				depthBrackets--
			}
		case ',':
			if depthAngles == 0 && depthParens == 0 && depthBraces == 0 && depthBrackets == 0 {
				sections = append(sections, text[start:index])
				start = index + 1
			}
		}
	}

	sections = append(sections, text[start:])
	return sections
}

func TypeParameters(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return []string{}
	}
	typeParametersNode := node.ChildByFieldName("type_parameters")
	if typeParametersNode == nil {
		return []string{}
	}
	return TypeParameterNames(shared.NodeText(typeParametersNode, source))
}
