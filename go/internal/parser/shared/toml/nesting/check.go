// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
)

// ErrTooDeep reports a document that nests deeper than the caller's limit.
// Callers treat the file as unparseable: a TOML decoder would recurse once
// per level and can overflow the goroutine stack, which is a fatal runtime
// error that recover cannot catch.
var ErrTooDeep = errors.New("toml nesting exceeds limit")

// ErrUnterminatedString reports a basic, literal, or multi-line string that
// never closes. Such a document is not valid TOML, so a decoder would reject
// it too; the guard rejects it first so the lexer never has to guess where
// the string was meant to end.
var ErrUnterminatedString = errors.New("toml string is not terminated")

// structural marks the bytes the scan acts on. Every other byte is skipped
// without entering the switch, which keeps the pass cheap on large files.
var structural = [256]bool{
	'\n': true, '#': true, '"': true, '\'': true, '.': true, '=': true,
	'[': true, ']': true, '{': true, '}': true, ',': true,
}

// frame is one open array or inline table.
type frame struct {
	// table is true for an inline table, whose entries are key/value pairs.
	table bool
	// base is the depth just inside the container. A comma or the closing
	// bracket drops back to it, which forgets the dotted key of the entry
	// that just ended.
	base int
}

// Check reports whether source nests deeper than limit before any TOML
// decoder sees it. It returns nil, an error wrapping ErrTooDeep, or an error
// wrapping ErrUnterminatedString. A negative limit behaves like zero.
//
// Depth at a point in the document is the number of arrays and inline tables
// that enclose it plus the number of dots in the dotted keys that lead to it.
// Both are recursion in the decoder: go-toml v2.3.1 recurses once per array
// or inline-table level and once per dotted-key segment, with no bound. So
// `a = 1` is depth 0, `a.b = [1]` is depth 2 at the 1, `x = {a.b = {c = 1}}`
// is depth 3 at the 1, and the table header `[a.b.c]` is depth 2. Header
// brackets do not count: a header is one level and cannot nest.
//
// Check is one linear pass with no allocation beyond a stack of at most
// limit+1 frames. Its lexer follows the TOML string rules exactly for every
// document a decoder accepts: comments, basic strings with escapes, literal
// strings, and multi-line strings whose closing delimiter may carry one or
// two extra quotes of content. Brackets, braces, and dots inside a string or
// comment never count. Where the lexer could disagree with a decoder, the
// decoder has already failed on an earlier byte, so nothing past that point
// is ever decoded.
func Check(source []byte, limit int) error {
	_, err := scan(source, max(limit, 0))
	return err
}

// scan walks source and returns the deepest depth it saw. It stops with
// ErrTooDeep as soon as the depth passes limit.
func scan(source []byte, limit int) (int, error) {
	var (
		stack    = make([]frame, 0, 8)
		depth    int
		deepest  int
		inKey    = true // a top-level line starts in key position
		inHeader bool
		// tableDepth is the depth of the latest table header. Top-level
		// lines under it start there.
		tableDepth int
	)
	deeper := func(at int) error {
		depth++
		deepest = max(deepest, depth)
		if depth > limit {
			return fmt.Errorf("%w: depth %d passes limit %d at byte %d", ErrTooDeep, depth, limit, at)
		}
		return nil
	}

	for i := 0; i < len(source); {
		c := source[i]
		if !structural[c] {
			i++
			continue
		}
		switch c {
		case '\n':
			if len(stack) == 0 {
				// A top-level expression and any table header end at the
				// newline. Inside an open array the line continues.
				if inHeader {
					tableDepth = depth
				}
				depth, inKey, inHeader = tableDepth, true, false
			}
			i++
		case '#':
			i = skipComment(source, i)
		case '"', '\'':
			end, ok := skipString(source, i)
			if !ok {
				return deepest, fmt.Errorf("%w: string opened at byte %d", ErrUnterminatedString, i)
			}
			i = end
		case '.':
			if inKey || inHeader {
				if err := deeper(i); err != nil {
					return deepest, err
				}
			}
			i++
		case '=':
			if !inHeader {
				inKey = false
			}
			i++
		case '[':
			switch {
			case inHeader:
				// The second bracket of [[array.table]]. Headers do not nest.
			case len(stack) == 0 && inKey:
				// A table header. Its depth is its own dots, not the
				// previous header's.
				inHeader, depth = true, 0
			default:
				if err := deeper(i); err != nil {
					return deepest, err
				}
				stack = append(stack, frame{base: depth})
				inKey = false
			}
			i++
		case '{':
			if !inHeader {
				if err := deeper(i); err != nil {
					return deepest, err
				}
				stack = append(stack, frame{table: true, base: depth})
				inKey = true
			}
			i++
		case ',':
			if n := len(stack); n > 0 {
				depth, inKey = stack[n-1].base, stack[n-1].table
			}
			i++
		case ']', '}':
			if n := len(stack); n > 0 && !inHeader {
				depth = stack[n-1].base - 1
				stack = stack[:n-1]
				inKey = false
			}
			i++
		default:
			// Unreachable: structural lists every byte the switch handles.
			i++
		}
	}
	return deepest, nil
}
