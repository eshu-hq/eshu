// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"regexp"
	"strings"
)

var (
	cypherPropRef  = regexp.MustCompile(`\b(\w+)\.(\w+)\b`)
	cypherParamRef = regexp.MustCompile(`\$(\w+)`)
	cypherIdent    = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

// parseIndexWriteExpr reduces one assigned Cypher expression to the value
// references the guard can measure: UNWIND row fields (through WITH aliases),
// scalar $params, string literals joined by a top-level +, and the
// identifiers it reads that it cannot resolve.
func parseIndexWriteExpr(expr string, scope exprScope) indexWriteExpr {
	var out indexWriteExpr
	trimmed := strings.TrimSpace(expr)

	// A bare identifier is the whole value: a row alias (the row is the
	// map), a WITH-lifted map, or something unresolvable.
	if cypherIdent.MatchString(trimmed) {
		if alias, ok := scope.fields[trimmed]; ok {
			out.rowVar, out.fields = alias.rowVar, []string{alias.field}
		} else if canonical, ok := scope.rowNames[trimmed]; ok {
			out.rowVar, out.wholeRow = canonical, true
		} else if !isCypherConstantWord(trimmed) {
			out.unknown = append(out.unknown, trimmed)
		}
		return out
	}

	masked := maskQuoted(expr)
	for _, loc := range cypherPropRef.FindAllStringSubmatchIndex(masked, -1) {
		base := masked[loc[2]:loc[3]]
		field := masked[loc[4]:loc[5]]
		if loc[0] > 0 && masked[loc[0]-1] == '$' {
			out.unknown = append(out.unknown, "$"+base) // $param.key subscripts are not measured
			continue
		}
		if next := byteAt(masked, loc[1]); next == '(' || next == '.' || (base[0] >= '0' && base[0] <= '9') {
			continue // a function namespace (apoc.text.join) or a number (1.5)
		}
		canonical, ok := scope.rowNames[base]
		if !ok {
			out.unknown = append(out.unknown, base)
			continue
		}
		if out.rowVar == "" {
			out.rowVar = canonical
		}
		if canonical == out.rowVar {
			out.fields = append(out.fields, field)
		}
	}
	for _, m := range cypherParamRef.FindAllStringSubmatch(masked, -1) {
		out.params = append(out.params, m[1])
	}
	for _, part := range splitTopLevelOn(expr, '+') {
		if part != expr {
			out.sum = true
		}
		if lit, ok := quotedLiteralBytes(part); ok && out.sum {
			out.constBytes += lit
		}
	}
	return out
}

// quotedLiteralBytes returns the content length of part when it is one
// quoted string literal.
func quotedLiteralBytes(part string) (int, bool) {
	part = strings.TrimSpace(part)
	if len(part) >= 2 && (part[0] == '\'' || part[0] == '"') && part[len(part)-1] == part[0] {
		return len(part) - 2, true
	}
	return 0, false
}

func isCypherConstantWord(word string) bool {
	switch strings.ToLower(word) {
	case "true", "false", "null":
		return true
	}
	return false
}

func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// quotedMask marks every byte of s that sits inside a quoted string.
func quotedMask(s string) []bool {
	mask := make([]bool, len(s))
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			mask[i] = true
			if c == '\\' && i+1 < len(s) {
				i++
				mask[i] = true
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			mask[i] = true
		}
	}
	return mask
}

// maskQuoted returns s with the bytes of every quoted string replaced by
// spaces, so reference scans cannot read text out of a literal.
func maskQuoted(s string) string {
	if !strings.ContainsAny(s, `'"`) {
		return s
	}
	mask := quotedMask(s)
	b := []byte(s)
	for i := range b {
		if mask[i] {
			b[i] = ' '
		}
	}
	return string(b)
}

// matchingBrace returns the index of the brace closing the one at open, or -1.
func matchingBrace(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTopLevel(s string) []string { return splitTopLevelOn(s, ',') }

// splitTopLevelOn splits s on sep outside parentheses, brackets, braces, and
// quoted strings.
func splitTopLevelOn(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
