// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pgarray

import "fmt"

// element is one parsed array member. null is set for an unquoted NULL; a
// quoted "NULL" is the four-character string.
type element struct {
	text string
	null bool
}

// parseLinearArray parses the text form Postgres emits for a one-dimensional
// array: `{` elements `}`, elements separated by a comma, each either
// double-quoted with backslash escapes or a bare run of characters up to the
// next comma or closing brace. Only representations produced by the server
// are supported: whitespace is significant (a bare ` a` is the element " a"),
// NULL is case-sensitive, and a nested `{` -- a multi-dimensional array -- is
// rejected as an error, never flattened.
//
// typ names the caller's Go type in the multi-dimensional error so a scan
// failure says which column shape did not fit.
func parseLinearArray(src, typ string) ([]element, error) {
	if len(src) < 1 || src[0] != '{' {
		return nil, fmt.Errorf("pgarray: unable to parse array; expected %q at offset %d", '{', 0)
	}
	if len(src) >= 2 && src[1] == '{' {
		return nil, fmt.Errorf("pgarray: cannot convert a multidimensional array to %s", typ)
	}
	i := 1
	if i < len(src) && src[i] == '}' {
		if i+1 != len(src) {
			return nil, unexpectedAt(src, i+1)
		}
		return []element{}, nil
	}
	var elems []element
	for {
		if i >= len(src) {
			return nil, expectedCloseAt(i)
		}
		switch src[i] {
		case '"':
			text, next, ok := parseQuoted(src, i+1)
			if !ok {
				return nil, expectedCloseAt(next)
			}
			elems = append(elems, element{text: text})
			i = next
		case '{':
			return nil, unexpectedAt(src, i)
		default:
			start := i
			for i < len(src) && src[i] != ',' && src[i] != '}' {
				i++
			}
			if i >= len(src) {
				return nil, expectedCloseAt(i)
			}
			if i == start {
				return nil, unexpectedAt(src, i)
			}
			if text := src[start:i]; text == "NULL" {
				elems = append(elems, element{null: true})
			} else {
				elems = append(elems, element{text: text})
			}
		}
		if i >= len(src) {
			return nil, expectedCloseAt(i)
		}
		switch src[i] {
		case ',':
			i++
		case '}':
			if i+1 != len(src) {
				return nil, unexpectedAt(src, i+1)
			}
			return elems, nil
		default:
			return nil, unexpectedAt(src, i)
		}
	}
}

// parseQuoted reads a double-quoted element whose opening quote sits at
// start-1. It returns the unescaped text and the index just past the closing
// quote; ok is false when the input ends before the element closes, in which
// case next is the length of src.
func parseQuoted(src string, start int) (text string, next int, ok bool) {
	buf := make([]byte, 0, 16)
	escape := false
	for i := start; i < len(src); i++ {
		c := src[i]
		switch {
		case escape:
			buf = append(buf, c)
			escape = false
		case c == '\\':
			escape = true
		case c == '"':
			return string(buf), i + 1, true
		default:
			buf = append(buf, c)
		}
	}
	return "", len(src), false
}

func unexpectedAt(src string, i int) error {
	return fmt.Errorf("pgarray: unable to parse array; unexpected %q at offset %d", src[i], i)
}

func expectedCloseAt(i int) error {
	return fmt.Errorf("pgarray: unable to parse array; expected %q at offset %d", '}', i)
}
