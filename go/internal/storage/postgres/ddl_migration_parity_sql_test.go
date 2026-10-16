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
	// Reserved words (PostgreSQL 18, Appendix C) that start an unnamed
	// table constraint, so they cannot be unquoted column names. EXCLUDE is
	// not reserved and stays a readable name.
	"primary": true, "unique": true, "check": true, "foreign": true, "like": true,
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

// stripSQLComments blanks "--" line comments and "/* */" block comments,
// keeping newlines so line numbers hold. Single-quoted literals,
// double-quoted identifiers, and dollar-quoted bodies ($$...$$ or
// $tag$...$tag$) are skipped whole, so a comment marker or an apostrophe
// inside one is body text. An unterminated quote runs to the end of sql.
func stripSQLComments(sql string) string {
	out := []byte(sql)
	for i := 0; i < len(out); i++ {
		switch c := out[i]; {
		case c == '\'' || c == '"':
			// A doubled quote ('it''s') closes here and reopens at once.
			end := strings.IndexByte(sql[i+1:], c)
			if end < 0 {
				return string(out)
			}
			i += 1 + end
		case c == '$':
			tag, ok := dollarQuoteTag(sql, i)
			if !ok {
				continue
			}
			end := strings.Index(sql[i+len(tag):], tag)
			if end < 0 {
				return string(out)
			}
			i += len(tag) + end + len(tag) - 1
		case c == '-' && i+1 < len(out) && out[i+1] == '-':
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

// dollarQuoteTag reports the dollar-quote tag that opens at s[i], which must
// be '$'. Per the PostgreSQL lexical rules a tag is "$", an optional
// identifier that does not start with a digit and holds no "$", and "$".
// A "$" that continues an identifier (a$$b) or starts a positional
// parameter ($1) opens no dollar quote.
func dollarQuoteTag(s string, i int) (string, bool) {
	if i > 0 && (ddlIdentByte(s[i-1]) || s[i-1] == '$') {
		return "", false
	}
	j := i + 1
	if j < len(s) && ddlIdentByte(s[j]) && (s[j] < '0' || s[j] > '9') {
		for j < len(s) && ddlIdentByte(s[j]) {
			j++
		}
	}
	if j >= len(s) || s[j] != '$' {
		return "", false
	}
	return s[i : j+1], true
}

// ddlIdentByte reports whether b can continue an unquoted identifier,
// apart from '$': a letter, a digit, an underscore, or a non-ASCII byte.
func ddlIdentByte(b byte) bool {
	return b == '_' || b >= 0x80 || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// ddlTopLevelText returns s with every parenthesized group and
// single-quoted literal blanked, so a keyword search sees only the
// top-level words of a column definition and not a CHECK body or a literal.
func ddlTopLevelText(s string) string {
	out := []byte(s)
	depth, inQuote := 0, false
	for i, c := range out {
		switch {
		case inQuote:
			inQuote = c != '\''
		case c == '\'':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0:
			continue
		}
		out[i] = ' '
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
