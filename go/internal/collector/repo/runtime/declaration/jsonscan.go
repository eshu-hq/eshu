// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/json"
	"errors"
	"strings"
)

// maxJSONDepth bounds how deeply a manifest may nest. A real package.json nests
// four or five levels; the cap turns a hostile "[[[[..." into an early,
// allocation-free failure instead of a deep recursion.
const maxJSONDepth = 64

// errMalformedJSON reports input the scanner refuses to read. It carries no
// position or content: the caller logs only that the file is unreadable.
var errMalformedJSON = errors.New("malformed json")

// jsonScanner walks a JSON document in place, by byte offset, and reports the
// 1-based line of each member it reads. It is written for a fixed set of keys
// in a file of unknown size: it validates the whole document but allocates
// only for a string it is asked to decode, never for a value it skips, so cost
// is time linear in the body and memory flat in it.
//
// It reads strict JSON plus "//" and "/* */" comments (JSONC-style), which
// some repositories keep in a manifest. It reads no other extension: a trailing
// comma, an unquoted key or a raw control byte in a string is malformed, and a
// malformed document yields no declaration at all, because the part before the
// error would pass for the whole file.
type jsonScanner struct {
	s     string
	i     int
	depth int
	// line is the 1-based line at byte offset lineAt. lineOf advances both, so
	// counting newlines is one pass over the body however many members ask.
	line   int
	lineAt int
}

func newJSONScanner(body string) *jsonScanner {
	return &jsonScanner{s: strings.TrimPrefix(body, "\xef\xbb\xbf"), line: 1}
}

// lineOf returns the 1-based line of byte offset off. Offsets must not go
// backwards, which holds because the scan only moves forward.
func (sc *jsonScanner) lineOf(off int) int {
	if off > sc.lineAt {
		sc.line += strings.Count(sc.s[sc.lineAt:off], "\n")
		sc.lineAt = off
	}
	return sc.line
}

// skipSpace skips JSON whitespace and comments. An unterminated block comment
// is malformed.
func (sc *jsonScanner) skipSpace() error {
	for sc.i < len(sc.s) {
		switch sc.s[sc.i] {
		case ' ', '\t', '\n', '\r':
			sc.i++
		case '/':
			if sc.i+1 >= len(sc.s) {
				return errMalformedJSON
			}
			switch sc.s[sc.i+1] {
			case '/':
				end := strings.IndexByte(sc.s[sc.i:], '\n')
				if end < 0 {
					sc.i = len(sc.s)
				} else {
					sc.i += end
				}
			case '*':
				end := strings.Index(sc.s[sc.i+2:], "*/")
				if end < 0 {
					return errMalformedJSON
				}
				sc.i += 2 + end + 2
			default:
				return errMalformedJSON
			}
		default:
			return nil
		}
	}
	return nil
}

// atEnd reports whether only whitespace and comments remain.
func (sc *jsonScanner) atEnd() (bool, error) {
	if err := sc.skipSpace(); err != nil {
		return false, err
	}
	return sc.i >= len(sc.s), nil
}

// expect consumes one byte after whitespace.
func (sc *jsonScanner) expect(c byte) error {
	if err := sc.skipSpace(); err != nil {
		return err
	}
	if sc.i >= len(sc.s) || sc.s[sc.i] != c {
		return errMalformedJSON
	}
	sc.i++
	return nil
}

// peek returns the next byte after whitespace, or 0 at the end.
func (sc *jsonScanner) peek() (byte, error) {
	if err := sc.skipSpace(); err != nil {
		return 0, err
	}
	if sc.i >= len(sc.s) {
		return 0, nil
	}
	return sc.s[sc.i], nil
}

// rawString reads one string and returns its text between the quotes and
// whether it holds an escape. It validates escapes and refuses a raw control
// byte, a newline included.
func (sc *jsonScanner) rawString() (text string, escaped bool, err error) {
	if sc.i >= len(sc.s) || sc.s[sc.i] != '"' {
		return "", false, errMalformedJSON
	}
	start := sc.i + 1
	for j := start; j < len(sc.s); j++ {
		switch c := sc.s[j]; {
		case c == '"':
			sc.i = j + 1
			return sc.s[start:j], escaped, nil
		case c == '\\':
			escaped = true
			j++
			if j >= len(sc.s) {
				return "", false, errMalformedJSON
			}
			switch sc.s[j] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				if j+4 >= len(sc.s) || !isHex4(sc.s[j+1:j+5]) {
					return "", false, errMalformedJSON
				}
				j += 4
			default:
				return "", false, errMalformedJSON
			}
		case c < 0x20:
			return "", false, errMalformedJSON
		}
	}
	return "", false, errMalformedJSON
}

func isHex4(s string) bool {
	for i := 0; i < 4; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// Decode bounds for an escaped string. A key or value this package matches is
// short, so a longer escaped string is returned still escaped: it holds a
// backslash, so it matches no key, and safeSpec rejects it as a value. That
// bounds the allocation of one decode, and so the cost of a hostile file that
// repeats a long escaped string, by these constants and not by the file.
const (
	maxEscapedKeyBytes   = 64
	maxEscapedValueBytes = 512
)

// stringValue reads one string and decodes its escapes, up to limit raw bytes
// when it has any. Only a key or value the caller needs comes here; an
// unescaped string is returned without a copy.
func (sc *jsonScanner) stringValue(limit int) (string, error) {
	text, escaped, err := sc.rawString()
	if err != nil {
		return "", err
	}
	if !escaped || len(text) > limit {
		return text, nil
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+text+`"`), &decoded); err != nil {
		return "", errMalformedJSON
	}
	return decoded, nil
}

// eachMember reads one object and calls fn for each member with its decoded key
// and the 1-based line of the key. fn must consume the member's value, with
// skipValue or its own reader, and may return an error to stop.
func (sc *jsonScanner) eachMember(fn func(key string, line int) error) error {
	if err := sc.expect('{'); err != nil {
		return err
	}
	sc.depth++
	if sc.depth > maxJSONDepth {
		return errMalformedJSON
	}
	defer func() { sc.depth-- }()
	c, err := sc.peek()
	if err != nil {
		return err
	}
	if c == '}' {
		sc.i++
		return nil
	}
	for {
		if _, err := sc.peek(); err != nil {
			return err
		}
		line := sc.lineOf(sc.i)
		key, err := sc.stringValue(maxEscapedKeyBytes)
		if err != nil {
			return err
		}
		if err := sc.expect(':'); err != nil {
			return err
		}
		if err := fn(key, line); err != nil {
			return err
		}
		c, err := sc.peek()
		if err != nil {
			return err
		}
		sc.i++
		switch c {
		case ',':
		case '}':
			return nil
		default:
			return errMalformedJSON
		}
	}
}

// skipValue reads and discards one value of any type, validating it.
func (sc *jsonScanner) skipValue() error {
	c, err := sc.peek()
	if err != nil {
		return err
	}
	switch {
	case c == '"':
		_, _, err = sc.rawString()
		return err
	case c == '{':
		return sc.eachMember(func(_ string, _ int) error { return sc.skipValue() })
	case c == '[':
		return sc.skipArray()
	case c == '-' || c >= '0' && c <= '9':
		return sc.skipNumber()
	case c == 't':
		return sc.skipLiteral("true")
	case c == 'f':
		return sc.skipLiteral("false")
	case c == 'n':
		return sc.skipLiteral("null")
	}
	return errMalformedJSON
}

func (sc *jsonScanner) skipArray() error {
	sc.i++ // '['
	sc.depth++
	if sc.depth > maxJSONDepth {
		return errMalformedJSON
	}
	defer func() { sc.depth-- }()
	c, err := sc.peek()
	if err != nil {
		return err
	}
	if c == ']' {
		sc.i++
		return nil
	}
	for {
		if err := sc.skipValue(); err != nil {
			return err
		}
		c, err := sc.peek()
		if err != nil {
			return err
		}
		sc.i++
		switch c {
		case ',':
		case ']':
			return nil
		default:
			return errMalformedJSON
		}
	}
}

func (sc *jsonScanner) skipLiteral(word string) error {
	if !strings.HasPrefix(sc.s[sc.i:], word) {
		return errMalformedJSON
	}
	sc.i += len(word)
	return nil
}

// skipNumber validates the JSON number grammar: -?(0|[1-9][0-9]*)(.[0-9]+)?([eE][+-]?[0-9]+)?
func (sc *jsonScanner) skipNumber() error {
	digits := func() int {
		n := 0
		for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
			sc.i++
			n++
		}
		return n
	}
	if sc.s[sc.i] == '-' {
		sc.i++
	}
	if sc.i < len(sc.s) && sc.s[sc.i] == '0' {
		sc.i++
	} else if digits() == 0 {
		return errMalformedJSON
	}
	if sc.i < len(sc.s) && sc.s[sc.i] == '.' {
		sc.i++
		if digits() == 0 {
			return errMalformedJSON
		}
	}
	if sc.i < len(sc.s) && (sc.s[sc.i] == 'e' || sc.s[sc.i] == 'E') {
		sc.i++
		if sc.i < len(sc.s) && (sc.s[sc.i] == '+' || sc.s[sc.i] == '-') {
			sc.i++
		}
		if digits() == 0 {
			return errMalformedJSON
		}
	}
	return nil
}
