// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import "strings"

// kotlinLooksLikeTypeName reports whether a bare identifier looks like a type
// name (starts with an upper-case letter), used to distinguish constructor
// calls from plain function calls during type inference.
func kotlinLooksLikeTypeName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	first := rune(name[0])
	return first >= 'A' && first <= 'Z'
}

// kotlinNormalizeParenthesizedReceivers collapses a parenthesized receiver that
// is immediately followed by a member access, turning `(a.b()).c` into
// `a.b().c`. It only unwraps groups whose contents are an identifier/call chain
// (no operators), so `(x as T).m` is preserved. The scan is repeated until no
// further group can be unwrapped.
func kotlinNormalizeParenthesizedReceivers(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}
	for {
		next, changed := unwrapLeadingReceiverGroup(normalized)
		if !changed {
			return normalized
		}
		normalized = next
	}
}

// unwrapLeadingReceiverGroup removes the first `(chain).` wrapper found in the
// expression and reports whether a change was made. A wrapper qualifies only
// when its contents form a plain identifier/call chain followed by `.`.
func unwrapLeadingReceiverGroup(value string) (string, bool) {
	for open := 0; open < len(value); open++ {
		if value[open] != '(' {
			continue
		}
		close := matchingParen(value, open)
		if close < 0 {
			return value, false
		}
		// The group must be immediately followed by a member access dot.
		if close+1 >= len(value) || value[close+1] != '.' {
			continue
		}
		inner := value[open+1 : close]
		if !isReceiverChain(inner) {
			continue
		}
		return value[:open] + inner + value[close+1:], true
	}
	return value, false
}

// isReceiverChain reports whether s is a plain identifier/call chain such as
// `a.b()` or `factory.create()` with balanced parentheses and no operators.
func isReceiverChain(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth < 0 {
				return false
			}
		case depth > 0:
			// Inside argument list: allow any character.
		case c == '.' || c == '_' || isAlphaNumeric(c):
			// Allowed chain characters at the top level.
		default:
			return false
		}
	}
	return depth == 0
}

func isAlphaNumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// matchingParen returns the index of the parenthesis that closes the one at
// openIndex, or -1 when unbalanced.
func matchingParen(value string, openIndex int) int {
	if openIndex < 0 || openIndex >= len(value) {
		return -1
	}
	depth := 0
	for index := openIndex; index < len(value); index++ {
		switch value[index] {
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

// kotlinStripWrappingParentheses removes one fully-enclosing pair of
// parentheses from an expression, leaving inner groups intact.
func kotlinStripWrappingParentheses(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '(' || trimmed[len(trimmed)-1] != ')' {
		return trimmed
	}

	depth := 0
	for index, char := range trimmed {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(trimmed)-1 {
				return trimmed
			}
		}
	}
	if depth != 0 {
		return trimmed
	}

	return strings.TrimSpace(trimmed[1 : len(trimmed)-1])
}

// kotlinImportAlias returns the simple-name alias derived from an import path,
// i.e. its last dot-separated segment.
func kotlinImportAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "."); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+1:])
	}
	return trimmed
}

// kotlinCallNameAllowed reports whether a member name is a real call target
// rather than a Kotlin keyword that can appear in call-shaped positions.
func kotlinCallNameAllowed(name string) bool {
	switch name {
	case "fun", "if", "for", "while", "when", "return", "class", "interface":
		return false
	default:
		return true
	}
}
