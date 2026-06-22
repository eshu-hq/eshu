package sql

import (
	"strings"
	"unicode"
)

// sqlSegment is one statement-sized slice of the original source with the byte
// offset where it began. Offsets let the extractor map recovered nodes back to
// original source line numbers.
type sqlSegment struct {
	text   string
	offset int
}

// splitSQLStatements segments SQL source into statement-sized fragments. The
// DerekStride grammar parses one statement reliably but degrades on
// concatenated or malformed input, and it cannot parse CREATE PROCEDURE at all.
// Segmenting on top-level statement boundaries lets every well-formed statement
// be parsed in isolation, recovers a malformed statement without losing its
// neighbours, and gives the procedure shim a bounded fragment to rewrite.
//
// Boundaries are: a top-level CREATE/ALTER keyword (paren depth zero, outside
// dollar-quoted bodies, line/block comments, and string literals), or the
// character after a top-level statement terminator. Dollar-quote tags, single
// and double quoted strings, backtick identifiers, and comments are skipped so
// embedded semicolons and keywords inside routine bodies never split a segment.
func splitSQLStatements(source string) []sqlSegment {
	segments := make([]sqlSegment, 0)
	start := 0
	depth := 0
	index := 0
	length := len(source)

	flush := func(end int) {
		if strings.TrimSpace(source[start:end]) != "" {
			segments = append(segments, sqlSegment{text: source[start:end], offset: start})
		}
		start = end
	}

	for index < length {
		switch {
		case strings.HasPrefix(source[index:], "--"):
			index += skipLineComment(source[index:])
		case strings.HasPrefix(source[index:], "/*"):
			index += skipBlockComment(source[index:])
		case source[index] == '\'':
			index += skipQuoted(source[index:], '\'')
		case source[index] == '"':
			index += skipQuoted(source[index:], '"')
		case source[index] == '`':
			index += skipQuoted(source[index:], '`')
		case source[index] == '$':
			if tag := sqlDollarQuoteTagAt(source[index:]); tag != "" {
				index += skipDollarQuoted(source[index:], tag)
				continue
			}
			index++
		case source[index] == '(':
			depth++
			index++
		case source[index] == ')':
			if depth > 0 {
				depth--
			}
			index++
		case source[index] == ';' && depth == 0:
			index++
			flush(index)
		case startsStatementBoundary(source, index, depth):
			if index > start {
				flush(index)
			}
			// A line-initial CREATE/ALTER is a hard boundary even when an
			// earlier malformed statement left an unbalanced paren open, so the
			// recovered statement begins with a clean paren depth.
			depth = 0
			index++
		default:
			index++
		}
	}
	flush(length)
	return segments
}

// startsStatementBoundary reports whether a new CREATE or ALTER statement
// begins at offset. At paren depth zero any word-boundary CREATE/ALTER is a
// boundary. When a malformed earlier statement left parens open (depth > 0),
// only a line-initial CREATE/ALTER is treated as a boundary, since valid SQL
// never opens a new top-level statement inside a balanced clause.
func startsStatementBoundary(source string, offset int, depth int) bool {
	if !startsKeyword(source, offset) {
		return false
	}
	if depth == 0 {
		return true
	}
	return atLineStart(source, offset)
}

// atLineStart reports whether offset is preceded only by horizontal whitespace
// back to a newline or the start of the source.
func atLineStart(source string, offset int) bool {
	for index := offset - 1; index >= 0; index-- {
		switch source[index] {
		case ' ', '\t', '\r':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

// startsKeyword reports whether a CREATE or ALTER keyword begins at offset with
// word boundaries on both sides.
func startsKeyword(source string, offset int) bool {
	if offset > 0 {
		prev := rune(source[offset-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' {
			return false
		}
	}
	rest := source[offset:]
	for _, keyword := range []string{"CREATE", "ALTER"} {
		if len(rest) < len(keyword) {
			continue
		}
		if strings.EqualFold(rest[:len(keyword)], keyword) {
			after := len(keyword)
			if after < len(rest) {
				next := rune(rest[after])
				if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
					continue
				}
			}
			return true
		}
	}
	return false
}

func skipLineComment(rest string) int {
	if end := strings.IndexByte(rest, '\n'); end >= 0 {
		return end + 1
	}
	return len(rest)
}

func skipBlockComment(rest string) int {
	if end := strings.Index(rest[2:], "*/"); end >= 0 {
		return end + 4
	}
	return len(rest)
}

// skipQuoted advances past a quoted span, treating a doubled quote character as
// an escaped literal quote rather than a terminator.
func skipQuoted(rest string, quote byte) int {
	index := 1
	for index < len(rest) {
		if rest[index] == quote {
			if index+1 < len(rest) && rest[index+1] == quote {
				index += 2
				continue
			}
			return index + 1
		}
		index++
	}
	return len(rest)
}

func skipDollarQuoted(rest string, tag string) int {
	if end := strings.Index(rest[len(tag):], tag); end >= 0 {
		return len(tag) + end + len(tag)
	}
	return len(rest)
}
