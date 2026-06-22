package ruby

import "strings"

// This file holds byte-level scanners that replace the legacy regular
// expressions used for Ruby call recognition. Each scanner mirrors one regex
// character class or sub-pattern so the recovered call set stays byte-identical
// to the prior implementation.

// rubyVisibilityKeyword returns the canonical visibility keyword when value is
// exactly public, private, or protected, else the empty string.
func rubyVisibilityKeyword(value string) string {
	switch strings.TrimSpace(value) {
	case "public", "private", "protected":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func rubyIsIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

func rubyIsIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func rubyIsLowerStartByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z')
}

// rubyIsConstantBodyByte matches the `[A-Za-z0-9_:]` characters allowed inside a
// scoped constant name.
func rubyIsConstantBodyByte(b byte) bool {
	return rubyIsIdentByte(b) || b == ':'
}

// rubyIsReceiverStart reports whether a dotted-call receiver may begin at index.
// It enforces the leading boundary `(?:^|[^A-Za-z0-9_:@])` and a receiver head of
// an identifier, instance variable, `self`, or capitalized constant.
func rubyIsReceiverStart(line string, index int) bool {
	if index > 0 {
		prev := line[index-1]
		if rubyIsIdentByte(prev) || prev == ':' || prev == '@' {
			return false
		}
	}
	return rubyReceiverHeadLen(line, index) > 0
}

// rubyReceiverHeadLen returns the length of the receiver head token at index, or
// zero. The head is `@?[A-Za-z_]\w*`, `self`, or a capitalized scoped constant.
func rubyReceiverHeadLen(line string, index int) int {
	if index >= len(line) {
		return 0
	}
	start := index
	cursor := index
	if line[cursor] == '@' {
		cursor++
		if cursor >= len(line) || !rubyIsIdentStartByte(line[cursor]) {
			return 0
		}
		cursor++
		for cursor < len(line) && rubyIsIdentByte(line[cursor]) {
			cursor++
		}
		return cursor - start
	}
	if line[cursor] >= 'A' && line[cursor] <= 'Z' {
		return rubyScanScopedConstant(line, cursor) - start
	}
	if rubyIsIdentStartByte(line[cursor]) {
		cursor++
		for cursor < len(line) && rubyIsIdentByte(line[cursor]) {
			cursor++
		}
		return cursor - start
	}
	return 0
}

// rubyScanScopedConstant returns the index after a `[A-Z][A-Za-z0-9_:]*`
// constant beginning at index.
func rubyScanScopedConstant(line string, index int) int {
	if index >= len(line) || line[index] < 'A' || line[index] > 'Z' {
		return index
	}
	cursor := index + 1
	for cursor < len(line) && rubyIsConstantBodyByte(line[cursor]) {
		cursor++
	}
	return cursor
}

// rubyScanDottedReceiver scans `head(.method)+` and returns the index after the
// final method token plus whether at least one `.method` segment was consumed.
func rubyScanDottedReceiver(line string, index int) (int, bool) {
	head := rubyReceiverHeadLen(line, index)
	if head == 0 {
		return index, false
	}
	cursor := index + head
	segments := 0
	for cursor < len(line) && line[cursor] == '.' {
		next := rubyScanMethodName(line, cursor+1)
		if next == cursor+1 {
			break
		}
		cursor = next
		segments++
	}
	return cursor, segments > 0
}

// rubyScanQualifiedChain scans `head(.method)+` for the qualified recognizer and
// returns the end index plus the number of `.method` segments consumed. Trailing
// `?!=` suffixes are not consumed here.
func rubyScanQualifiedChain(line string, index int) (int, int) {
	head := rubyReceiverHeadLen(line, index)
	if head == 0 {
		return index, 0
	}
	cursor := index + head
	segments := 0
	for cursor < len(line) && line[cursor] == '.' {
		next := rubyScanBareMethodName(line, cursor+1)
		if next == cursor+1 {
			break
		}
		cursor = next
		segments++
	}
	return cursor, segments
}

// rubyScanMethodName returns the index after a `[A-Za-z_]\w*[!?=]?` method name.
func rubyScanMethodName(line string, index int) int {
	end := rubyScanBareMethodName(line, index)
	if end == index {
		return index
	}
	if end < len(line) {
		switch line[end] {
		case '!', '?', '=':
			end++
		}
	}
	return end
}

// rubyScanBareMethodName returns the index after a `[A-Za-z_]\w*` method name
// without a trailing predicate or writer suffix.
func rubyScanBareMethodName(line string, index int) int {
	if index >= len(line) || !rubyIsIdentStartByte(line[index]) {
		return index
	}
	cursor := index + 1
	for cursor < len(line) && rubyIsIdentByte(line[cursor]) {
		cursor++
	}
	return cursor
}

// rubyScanCallMethodName returns the index after a `[A-Za-z_]\w*[!?=]?` token for
// the scoped-call recognizer.
func rubyScanCallMethodName(line string, index int) int {
	return rubyScanMethodName(line, index)
}

// rubyScanLowerCallName returns the index after a `[a-z_]\w*[!?]?` lowercase
// method name, matching the receiverless recognizers.
func rubyScanLowerCallName(line string, index int) int {
	if index >= len(line) || !rubyIsLowerStartByte(line[index]) {
		return index
	}
	cursor := index + 1
	for cursor < len(line) && rubyIsIdentByte(line[cursor]) {
		cursor++
	}
	if cursor < len(line) && (line[cursor] == '!' || line[cursor] == '?') {
		cursor++
	}
	return cursor
}

// rubyScanParenArgs scans a balanced single-level `(...)` starting at the open
// paren at index. It returns the index after the closing paren and the inner
// text. The legacy pattern used `[^()]*`, so the first inner `(` or `)` ends the
// content; an unmatched run returns ok=false.
func rubyScanParenArgs(line string, index int) (int, string, bool) {
	if index >= len(line) || line[index] != '(' {
		return index, "", false
	}
	cursor := index + 1
	start := cursor
	for cursor < len(line) {
		switch line[cursor] {
		case ')':
			return cursor + 1, line[start:cursor], true
		case '(':
			return index, "", false
		}
		cursor++
	}
	return index, "", false
}

// rubyTrailingNonComment returns the same-line tail after index up to the first
// `#` comment marker, mirroring the `[^#]+` trailing capture.
func rubyTrailingNonComment(line string, index int) string {
	rest := line[index:]
	if hash := strings.IndexByte(rest, '#'); hash >= 0 {
		rest = rest[:hash]
	}
	return rest
}

// rubyQualifiedBoundaryOK reports whether the qualified call is followed by an
// allowed boundary `(?:\s*\(|\b|[\s;])`. A word boundary holds when the next
// byte is a non-identifier byte or end of line.
func rubyQualifiedBoundaryOK(line string, boundary int) bool {
	if boundary >= len(line) {
		return true
	}
	next := line[boundary]
	if next == ' ' || next == '\t' || next == ';' || next == '(' {
		return true
	}
	return !rubyIsIdentByte(next)
}

// rubyReceiverlessAssignmentBoundary reports whether `= name` is terminated by
// `(?:$|[#;,)])`.
func rubyReceiverlessAssignmentBoundary(line string, end int) bool {
	if end >= len(line) {
		return true
	}
	switch line[end] {
	case '#', ';', ',', ')':
		return true
	default:
		return false
	}
}

func rubyIsReceiverlessPrefixByte(b byte) bool {
	return rubyIsIdentByte(b) || b == ':' || b == '@' || b == '.'
}

func rubyIsBarePrefixByte(b byte) bool {
	return rubyIsIdentByte(b) || b == ':' || b == '@'
}

// rubyBareCallKeywords is the fixed receiverless method set recognized by the
// bare-call pattern, ordered longest-first so prefixes do not shadow longer
// names.
var rubyBareCallKeywords = []string{
	"require_relative",
	"require",
	"load",
	"include",
	"extend",
	"attr_accessor",
	"attr_reader",
	"attr_writer",
	"define_method",
	"define_singleton_method",
	"instance_method",
	"instance_eval",
	"cache_method",
	"puts",
	"sleep",
	"method",
	"public_send",
	"send",
	"super",
	"bind",
}

// rubyMatchBareCallKeyword returns the matched keyword and its end index when one
// of the fixed bare-call method names begins at index with a word boundary.
func rubyMatchBareCallKeyword(line string, index int) (string, int) {
	for _, keyword := range rubyBareCallKeywords {
		end := index + len(keyword)
		if end > len(line) {
			continue
		}
		if line[index:end] != keyword {
			continue
		}
		if end < len(line) && rubyIsIdentByte(line[end]) {
			continue
		}
		return keyword, end
	}
	return "", index
}
