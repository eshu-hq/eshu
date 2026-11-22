// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"errors"
	"strings"
)

// errUnterminated reports SQL the scrubber cannot read to the end, so the
// classifier fails closed instead of guessing where a literal or comment stops.
var errUnterminated = errors.New("unterminated string literal, quoted identifier, or comment")

// errQuotedBoolean reports a quoted identifier named true or false. Scrub turns
// quotes into spaces, so the dead-branch pass would read the column as the
// boolean literal and could drop a live read; the classifier fails closed.
var errQuotedBoolean = errors.New(`quoted identifier named "true" or "false", which reads as the boolean literal`)

// scrub reads one SQL text and returns two equal-length views of it:
//
//   - clean keeps string literals, so fact kinds can be read from them;
//   - masked replaces every string literal's content with 'x' characters, so a
//     keyword or a parenthesis inside a literal can never be read as SQL.
//
// Both views blank comments, so "--" inside a literal stays literal and a real
// comment never hides the rest of a line. Both unquote quoted identifiers by
// turning the quotes into spaces, so "fact_records" reads as the relation it
// names. Equal lengths let a span found in one view blank the other.
func scrub(sql string) (clean, masked string, err error) {
	cleanBytes := []byte(sql)
	maskedBytes := []byte(sql)
	blank := func(from, to int) {
		for i := from; i < to; i++ {
			if sql[i] != '\n' {
				cleanBytes[i], maskedBytes[i] = ' ', ' '
			}
		}
	}
	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '\'':
			end, ok := closingQuote(sql, i, '\'')
			if !ok {
				return "", "", errUnterminated
			}
			for j := i + 1; j < end; j++ {
				maskedBytes[j] = 'x'
			}
			i = end + 1
		case sql[i] == '"':
			end, ok := closingQuote(sql, i, '"')
			if !ok {
				return "", "", errUnterminated
			}
			if name := strings.ToLower(sql[i+1 : end]); name == "true" || name == "false" {
				return "", "", errQuotedBoolean
			}
			cleanBytes[i], maskedBytes[i] = ' ', ' '
			cleanBytes[end], maskedBytes[end] = ' ', ' '
			i = end + 1
		case strings.HasPrefix(sql[i:], "--"):
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				end = len(sql) - i
			}
			blank(i, i+end)
			i += end
		case strings.HasPrefix(sql[i:], "/*"):
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return "", "", errUnterminated
			}
			blank(i, i+2+end+2)
			i += 2 + end + 2
		case sql[i] == '$':
			tagEnd := dollarTagEnd(sql, i)
			if tagEnd < 0 {
				i++
				continue
			}
			tag := sql[i : tagEnd+1]
			end := strings.Index(sql[tagEnd+1:], tag)
			if end < 0 {
				return "", "", errUnterminated
			}
			bodyStart, bodyEnd := tagEnd+1, tagEnd+1+end
			for j := bodyStart; j < bodyEnd; j++ {
				maskedBytes[j] = 'x'
			}
			i = bodyEnd + len(tag)
		default:
			i++
		}
	}
	return string(cleanBytes), string(maskedBytes), nil
}

// closingQuote returns the index of the quote that closes the one at open,
// treating a doubled quote as an escape.
func closingQuote(sql string, open int, quote byte) (int, bool) {
	for i := open + 1; i < len(sql); i++ {
		if sql[i] != quote {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			i++
			continue
		}
		return i, true
	}
	return 0, false
}

// dollarTagEnd returns the index of the closing "$" of a dollar-quote tag that
// starts at start ($$ or $name$), or -1 when the "$" starts a bind placeholder.
func dollarTagEnd(sql string, start int) int {
	for i := start + 1; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '$':
			return i
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > start+1 && c >= '0' && c <= '9'):
		default:
			return -1
		}
	}
	return -1
}
