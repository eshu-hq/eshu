// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import "bytes"

// valueScan is the outcome of walking one value's bytes once. It tracks
// bracket depth and string state without recursion, so a deeply nested or
// unterminated value costs one pass and constant memory.
type valueScan struct {
	depth     int  // net open [ and { after the line; never negative
	badString bool // an unterminated string or a negative depth was seen
	scanned   int  // bytes this walk examined
}

// scanValueBytes advances depth across one line of TOML value text. Strings
// are single-line (basic strings honour backslash escapes, literal strings do
// not); a string that is still open at the end of the line is recorded as
// badString rather than carried to the next line. A # outside a string ends
// the line. Triple-quoted strings never reach this function: the driver
// rejects any file that contains them before scanning.
func scanValueBytes(value []byte, depth int) valueScan {
	scan := valueScan{depth: depth}
	var quote byte
	for i := 0; i < len(value); i++ {
		ch := value[i]
		scan.scanned++
		if quote != 0 {
			if quote == '"' && ch == '\\' {
				i++
				scan.scanned++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '[', '{':
			scan.depth++
		case ']', '}':
			scan.depth--
			if scan.depth < 0 {
				scan.depth = 0
				scan.badString = true
			}
		case '#':
			return scan
		}
	}
	if quote != 0 {
		scan.badString = true
	}
	return scan
}

// splitLine returns the next line of source from pos without its terminator
// and the position after it. A trailing carriage return is dropped so CRLF
// files read like LF files.
func splitLine(source []byte, pos int) (line []byte, next int) {
	rest := source[pos:]
	if index := bytes.IndexByte(rest, '\n'); index >= 0 {
		line = rest[:index]
		next = pos + index + 1
	} else {
		line = rest
		next = len(source)
	}
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	return line, next
}

// isBareKeyByte reports whether ch may appear in a bare TOML key.
func isBareKeyByte(ch byte) bool {
	return ch == '_' || ch == '-' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// splitBareKeyValue splits `key = value` where key is a bare key. Quoted or
// dotted keys, and lines without `=`, return ok=false so the caller treats
// them as an unrecognised construct instead of guessing.
func splitBareKeyValue(line []byte) (key, value []byte, ok bool) {
	end := 0
	for end < len(line) && isBareKeyByte(line[end]) {
		end++
	}
	if end == 0 {
		return nil, nil, false
	}
	rest := bytes.TrimLeft(line[end:], " \t")
	if len(rest) == 0 || rest[0] != '=' {
		return nil, nil, false
	}
	return line[:end], bytes.TrimLeft(rest[1:], " \t"), true
}

// restIsEmptyOrComment reports whether only whitespace or a # comment
// follows.
func restIsEmptyOrComment(rest []byte) bool {
	rest = bytes.TrimLeft(rest, " \t")
	return len(rest) == 0 || rest[0] == '#'
}

// parseBasicString reads one double-quoted string at the start of value. It
// accepts no escapes and no control bytes, and it examines at most max+2
// bytes, so a giant value cannot make this pass expensive. The returned rest
// is the text after the closing quote.
func parseBasicString(value []byte, max int) (content, rest []byte, ok bool) {
	if len(value) < 2 || value[0] != '"' {
		return nil, nil, false
	}
	limit := len(value)
	if limit > max+2 {
		limit = max + 2
	}
	for i := 1; i < limit; i++ {
		ch := value[i]
		switch {
		case ch == '"':
			return value[1:i], value[i+1:], true
		case ch == '\\' || ch < 0x20 || ch == 0x7f:
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// isNameChar reports whether ch is allowed in a package name.
func isNameChar(ch byte) bool {
	return ch == '.' || ch == '_' || ch == '-' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func isAlnum(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func isDigit(ch byte) bool { return ch >= '0' && ch <= '9' }

// validUVName accepts the documented package-name grammar: an alphanumeric
// start followed by alphanumerics, dots, underscores, and hyphens.
func validUVName(name []byte) bool {
	if len(name) == 0 || len(name) > uvLockMaxName || !isAlnum(name[0]) {
		return false
	}
	for _, ch := range name {
		if !isNameChar(ch) {
			return false
		}
	}
	return true
}

// validUVVersion accepts a PEP 440 shaped version: a digit start followed by
// alphanumerics, dots, underscores, hyphens, plus signs, and bangs.
func validUVVersion(version []byte) bool {
	if len(version) == 0 || len(version) > uvLockMaxVersion || !isDigit(version[0]) {
		return false
	}
	for _, ch := range version {
		if !isNameChar(ch) && ch != '+' && ch != '!' {
			return false
		}
	}
	return true
}

// startsPackageHeader reports whether a line opens with "[" (any run of
// brackets and spaces) and then the word `package`, as every package header
// form does ([[package]], [package], [package.x], a trailing comment). A bare
// word is never part of a valid TOML value, so such a line ends a value left
// open by mistake. It walks the leading run of brackets and blanks once, so
// the cost is linear in the line however much whitespace sits inside the
// brackets (a valid header may carry any amount).
func startsPackageHeader(trimmed []byte) bool {
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return false
	}
	i := 0
	for i < len(trimmed) && (trimmed[i] == '[' || trimmed[i] == ' ' || trimmed[i] == '\t') {
		i++
	}
	return bytes.HasPrefix(trimmed[i:], []byte("package"))
}

// bareDottedKey reports whether a header's content is one or more bare keys
// joined by dots, the only header form uv writes.
func bareDottedKey(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	start := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '.' {
			if i == start {
				return false
			}
			start = i + 1
			continue
		}
		if !isBareKeyByte(content[i]) {
			return false
		}
	}
	return true
}

// splitHeader parses `[a.b]` or `[[a.b]]`, returning the bracketed text, what
// follows, and whether it was an array header. ok=false when the closing
// brackets do not match the opening ones.
func splitHeader(trimmed []byte) (content, rest []byte, array, ok bool) {
	if bytes.HasPrefix(trimmed, []byte("[[")) {
		end := bytes.Index(trimmed, []byte("]]"))
		if end < 0 {
			return nil, nil, true, false
		}
		content = bytes.TrimSpace(trimmed[2:end])
		rest = trimmed[end+2:]
		if bytes.IndexByte(content, '[') >= 0 || bytes.IndexByte(content, ']') >= 0 || !restIsEmptyOrComment(rest) {
			return nil, nil, true, false
		}
		return content, rest, true, true
	}
	end := bytes.IndexByte(trimmed, ']')
	if end < 0 {
		return nil, nil, false, false
	}
	content = bytes.TrimSpace(trimmed[1:end])
	rest = trimmed[end+1:]
	if bytes.IndexByte(content, '[') >= 0 || !restIsEmptyOrComment(rest) {
		return nil, nil, false, false
	}
	return content, rest, false, true
}
