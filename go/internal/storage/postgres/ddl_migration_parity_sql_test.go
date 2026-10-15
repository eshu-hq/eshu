// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
)

// SQL text helpers for the Go-DDL versus numbered-migration parity guard
// (#7792). They are pure functions over SQL text.

var ddlPlainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

// ddlKeywordNames are words the name patterns can capture when the real name
// is joined on at runtime, as in "ALTER TABLE IF EXISTS " + table. A name
// that folds to one of them is unreadable, not an object.
var ddlKeywordNames = map[string]bool{
	"if": true, "not": true, "exists": true, "only": true, "column": true,
	"concurrently": true, "constraint": true, "on": true,
}

// normalizeDDLIdent folds a possibly schema-qualified identifier the way
// PostgreSQL does: the schema is dropped, a quoted name keeps its case, and
// an unquoted name is lowercased. It reports false for anything that is not
// a plain or quoted identifier, such as a fmt verb, and for an unquoted
// keyword from ddlKeywordNames.
func normalizeDDLIdent(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	var parts []string
	inQuote, start := false, 0
	for i := 0; i < len(raw); i++ {
		switch {
		case raw[i] == '"':
			inQuote = !inQuote
		case raw[i] == '.' && !inQuote:
			parts = append(parts, raw[start:i])
			start = i + 1
		}
	}
	parts = append(parts, raw[start:])
	name := ""
	for _, part := range parts {
		if len(part) >= 3 && part[0] == '"' && part[len(part)-1] == '"' {
			name = strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)
			continue
		}
		if !ddlPlainIdent.MatchString(part) {
			return "", false
		}
		name = strings.ToLower(part)
	}
	if ddlKeywordNames[strings.ToLower(name)] && !strings.HasSuffix(raw, `"`) {
		return "", false
	}
	return name, true
}

// stripSQLComments blanks "--" line comments and "/* */" block comments
// outside single-quoted literals, keeping newlines so line numbers hold.
func stripSQLComments(sql string) string {
	out := []byte(sql)
	inQuote := false
	for i := 0; i < len(out); i++ {
		switch {
		case inQuote:
			if out[i] == '\'' {
				inQuote = false
			}
		case out[i] == '\'':
			inQuote = true
		case out[i] == '-' && i+1 < len(out) && out[i+1] == '-':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case out[i] == '/' && i+1 < len(out) && out[i+1] == '*':
			end := strings.Index(string(out[i+2:]), "*/")
			stop := len(out)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			for ; i < stop; i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			i--
		}
	}
	return string(out)
}

// balancedParenBody returns the text inside the parenthesis that opens s,
// honouring nested parentheses and single-quoted literals.
func balancedParenBody(s string) (string, bool) {
	depth, inQuote := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote:
			inQuote = c != '\''
		case c == '\'':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// splitTopLevelCommas splits s at commas outside parentheses and
// single-quoted literals.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth, inQuote, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote:
			inQuote = c != '\''
		case c == '\'':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// ddlFirstLine returns s up to its first newline.
func ddlFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
