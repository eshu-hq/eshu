// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"bufio"
	"strings"
	"unicode"
)

// LangTOML is the canonical lang value emitted for pyproject.toml, Pipfile,
// and poetry.lock payloads.
const LangTOML = "python_toml"

// tomlSection captures one TOML table or array-of-tables with its leaf
// scalar/composite values, in source order. The minimal scanner only
// understands the shapes required by pyproject.toml, Pipfile, and
// poetry.lock; multi-line strings and dotted-key shorthand outside what
// those manifests use will surface as malformed entries through the
// dependency-table consumers.
type tomlSection struct {
	Header    string   // canonical "a.b.c" header; "" means top-level.
	IsArray   bool     // true for [[name]] array entries.
	Index     int      // 0-based index within an array-of-tables block.
	StartLine int      // 1-based file line for diagnostics.
	Keys      []string // declared key order.
	RawValues map[string]string
	Values    map[string]tomlValue
	Subtables []*tomlSection // child tables that scope under this entry.
	Parent    *tomlSection
}

// tomlValue is the parsed value attached to one key. Either Scalar (a TOML
// string, number, or bool literal stored as its source text) or InlineTable
// (key/value pairs from `{ ... }`) carries content.
type tomlValue struct {
	Scalar      string
	StringValue string
	IsString    bool
	Array       []string
	InlineTable map[string]tomlValue
	InlineKeys  []string
	LineNumber  int
	IsArray     bool
	IsInline    bool
}

// scanTOML reads source bytes into a flat ordered list of sections. The
// caller decides how to interpret nested tables (e.g. [package.source] under
// the last [[package]] array entry); the scanner only normalizes header
// names and key/value parsing.
func scanTOML(source string) ([]*tomlSection, error) {
	sections := []*tomlSection{{Header: "", RawValues: map[string]string{}, Values: map[string]tomlValue{}}}
	current := sections[0]

	scanner := bufio.NewScanner(strings.NewReader(source))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		line := stripTOMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
			header := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
			section := &tomlSection{
				Header:    header,
				IsArray:   true,
				StartLine: lineNumber,
				RawValues: map[string]string{},
				Values:    map[string]tomlValue{},
			}
			sections = append(sections, section)
			current = section
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			header := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			section := &tomlSection{
				Header:    header,
				StartLine: lineNumber,
				RawValues: map[string]string{},
				Values:    map[string]tomlValue{},
			}
			sections = append(sections, section)
			current = section
			continue
		}
		// Key = value (possibly spanning multiple lines if the value is an
		// array opened with `[` and unclosed). Read continuation lines until
		// the array closes.
		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			// Track as a malformed key so callers can preserve missing-evidence
			// semantics; we still record line number for diagnostics.
			current.Keys = append(current.Keys, "__malformed__"+itoa(lineNumber))
			current.RawValues["__malformed__"+itoa(lineNumber)] = trimmed
			current.Values["__malformed__"+itoa(lineNumber)] = tomlValue{
				Scalar:     trimmed,
				LineNumber: lineNumber,
			}
			continue
		}

		if needsContinuation(value) {
			builder := strings.Builder{}
			builder.WriteString(value)
			for scanner.Scan() {
				lineNumber++
				next := stripTOMLComment(scanner.Text())
				builder.WriteByte('\n')
				builder.WriteString(next)
				if !needsContinuation(builder.String()) {
					break
				}
			}
			value = builder.String()
		}

		parsed := parseTOMLValue(value, lineNumber)
		if _, exists := current.Values[key]; !exists {
			current.Keys = append(current.Keys, key)
		}
		current.Values[key] = parsed
		current.RawValues[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

// stripTOMLComment removes a trailing `#` comment from a TOML line, while
// keeping `#` characters that appear inside strings.
func stripTOMLComment(line string) string {
	inString := false
	stringRune := byte(0)
	escape := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == stringRune {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringRune = ch
			continue
		}
		if ch == '#' {
			return line[:i]
		}
	}
	return line
}

func splitKeyValue(trimmed string) (string, string, bool) {
	index := strings.Index(trimmed, "=")
	if index < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:index])
	value := strings.TrimSpace(trimmed[index+1:])
	if key == "" {
		return "", "", false
	}
	if value == "" {
		// `key =` on its own line is malformed for these manifests.
		return "", "", false
	}
	if firstRune, _ := firstRune(key); firstRune == 0 {
		return "", "", false
	}
	return unquoteKey(key), value, true
}

func unquoteKey(key string) string {
	if len(key) >= 2 {
		first := key[0]
		last := key[len(key)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return key[1 : len(key)-1]
		}
	}
	return key
}

// needsContinuation returns true when the running value text has an
// unbalanced `[` or `{` such that the next line must be appended before the
// value is complete.
func needsContinuation(text string) bool {
	bracket, brace := 0, 0
	inString := false
	stringRune := byte(0)
	escape := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == stringRune {
				inString = false
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inString = true
			stringRune = ch
		case '[':
			bracket++
		case ']':
			bracket--
		case '{':
			brace++
		case '}':
			brace--
		}
	}
	return bracket > 0 || brace > 0
}

func parseTOMLValue(raw string, lineNumber int) tomlValue {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tomlValue{LineNumber: lineNumber}
	}
	if strings.HasPrefix(trimmed, "[") {
		return tomlValue{
			Array:      splitArrayElements(trimmed),
			IsArray:    true,
			LineNumber: lineNumber,
		}
	}
	if strings.HasPrefix(trimmed, "{") {
		entries, keys := splitInlineTable(trimmed)
		return tomlValue{
			InlineTable: entries,
			InlineKeys:  keys,
			IsInline:    true,
			LineNumber:  lineNumber,
		}
	}
	if len(trimmed) >= 2 {
		first := trimmed[0]
		last := trimmed[len(trimmed)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return tomlValue{
				Scalar:      trimmed,
				StringValue: unquoteString(trimmed),
				IsString:    true,
				LineNumber:  lineNumber,
			}
		}
	}
	return tomlValue{Scalar: trimmed, LineNumber: lineNumber}
}

func unquoteString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	first := value[0]
	if first != '"' && first != '\'' {
		return value
	}
	return value[1 : len(value)-1]
}

// splitArrayElements returns the string-literal/inline-table elements of a
// TOML array. Nested arrays/inline tables are returned as their source slice
// so callers can re-parse them with parseTOMLValue if needed.
func splitArrayElements(text string) []string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil
	}
	inner := text[1 : len(text)-1]
	parts := splitTopLevelCommas(inner)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		piece := strings.TrimSpace(part)
		if piece == "" {
			continue
		}
		piece = unquoteString(piece)
		out = append(out, piece)
	}
	return out
}

// splitInlineTable parses `{ key = value, key = value }` into an ordered key
// list and a value map. Nested arrays/inline tables are preserved as their
// scalar source.
func splitInlineTable(text string) (map[string]tomlValue, []string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") || !strings.HasSuffix(text, "}") {
		return nil, nil
	}
	inner := text[1 : len(text)-1]
	pairs := splitTopLevelCommas(inner)
	values := make(map[string]tomlValue, len(pairs))
	keys := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		trimmed := strings.TrimSpace(pair)
		if trimmed == "" {
			continue
		}
		index := strings.Index(trimmed, "=")
		if index < 0 {
			continue
		}
		key := unquoteKey(strings.TrimSpace(trimmed[:index]))
		value := strings.TrimSpace(trimmed[index+1:])
		parsed := parseTOMLValue(value, 0)
		if _, exists := values[key]; !exists {
			keys = append(keys, key)
		}
		values[key] = parsed
	}
	return values, keys
}

// splitTopLevelCommas returns the comma-separated chunks of one TOML array
// or inline-table body, respecting nested brackets/braces and strings.
func splitTopLevelCommas(text string) []string {
	parts := []string{}
	bracket, brace := 0, 0
	inString := false
	stringRune := byte(0)
	escape := false
	start := 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == stringRune {
				inString = false
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inString = true
			stringRune = ch
		case '[':
			bracket++
		case ']':
			bracket--
		case '{':
			brace++
		case '}':
			brace--
		case ',':
			if bracket == 0 && brace == 0 {
				parts = append(parts, text[start:i])
				start = i + 1
			}
		}
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
}

func firstRune(value string) (rune, bool) {
	for _, r := range value {
		if !unicode.IsSpace(r) {
			return r, true
		}
	}
	return 0, false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
