// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// literal is one required text fragment of a regular expression. Each element
// is the set of runes the regular expression accepts at that position: one
// rune for a case-sensitive literal, the whole Unicode simple-fold orbit for a
// case-insensitive one (so "k" also accepts U+212A KELVIN SIGN and "s" also
// accepts U+017F LONG S).
//
// before and after record a word boundary (\b) that the regular expression
// places directly against the fragment. Go's \b is ASCII-only: a word
// character is [0-9A-Za-z_] and every other rune, including every non-ASCII
// letter, is a non-word character.
type literal struct {
	elems  [][]rune
	before bool
	after  bool
}

// key identifies a literal so that identical requirements share one scan.
func (l literal) key() string {
	var b strings.Builder
	if l.before {
		b.WriteByte('<')
	}
	for _, e := range l.elems {
		b.WriteByte('[')
		for _, r := range e {
			b.WriteString(strconv.Itoa(int(r)))
			b.WriteByte(',')
		}
		b.WriteByte(']')
	}
	if l.after {
		b.WriteByte('>')
	}
	return b.String()
}

// String renders the literal for diagnostics: a folded element is shown by
// its first ASCII-lowercase member followed by "~" once per literal, and the
// word boundaries are shown as \b.
func (l literal) String() string {
	var b strings.Builder
	folded := false
	if l.before {
		b.WriteString(`\b`)
	}
	b.WriteByte('"')
	for _, e := range l.elems {
		r := e[0]
		if len(e) > 1 {
			folded = true
			for _, c := range e {
				if c < utf8.RuneSelf && unicode.IsLower(c) {
					r = c
					break
				}
			}
		}
		b.WriteString(escapeRune(r))
	}
	b.WriteByte('"')
	if folded {
		b.WriteByte('~')
	}
	if l.after {
		b.WriteString(`\b`)
	}
	return b.String()
}

func escapeRune(r rune) string {
	q := strconv.QuoteToASCII(string(r))
	return q[1 : len(q)-1]
}

// matcher finds a compiled literal in text.
type matcher struct {
	elems  []matchElem
	plain  string // set when every element accepts exactly one rune
	first  [utf8.RuneSelf * 2]bool
	second [utf8.RuneSelf * 2]bool // first bytes of the second element; unused for one-element literals
	before bool
	after  bool
}

// matchElem accepts one position of a literal: an ASCII bitset plus the
// non-ASCII runes of the orbit.
type matchElem struct {
	lo, hi uint64
	other  []rune
}

func (e *matchElem) accepts(b byte) bool {
	if b < 64 {
		return e.lo&(1<<b) != 0
	}
	return e.hi&(1<<(b-64)) != 0
}

func (e *matchElem) acceptsRune(r rune) bool {
	for _, o := range e.other {
		if o == r {
			return true
		}
	}
	return false
}

func newMatcher(l literal) *matcher {
	m := &matcher{before: l.before, after: l.after}
	plain := true
	var plainBytes []byte
	for i, orbit := range l.elems {
		var e matchElem
		for _, r := range orbit {
			if r < utf8.RuneSelf {
				if r < 64 {
					e.lo |= 1 << (r & 63)
				} else {
					e.hi |= 1 << ((r - 64) & 63)
				}
			} else {
				e.other = append(e.other, r)
			}
		}
		m.elems = append(m.elems, e)
		if len(orbit) != 1 {
			plain = false
		} else if plain {
			plainBytes = utf8.AppendRune(plainBytes, orbit[0])
		}
		if i < 2 {
			var enc [utf8.UTFMax]byte
			for _, r := range orbit {
				utf8.EncodeRune(enc[:], r)
				if i == 0 {
					m.first[enc[0]] = true
				} else {
					m.second[enc[0]] = true
				}
			}
		}
	}
	if plain {
		m.plain = string(plainBytes)
	}
	return m
}

// found reports whether text holds an occurrence of the literal.
func (m *matcher) found(text string) bool {
	if m.plain != "" {
		for off := 0; ; {
			i := strings.Index(text[off:], m.plain)
			if i < 0 {
				return false
			}
			i += off
			if !m.before && !m.after || m.boundaryHolds(text, i, i+len(m.plain)) {
				return true
			}
			off = i + 1
		}
	}
	// A one-element literal has no second byte to test; every first-byte
	// candidate goes straight to matchAt.
	needSecond := len(m.elems) > 1
	for i := 0; i < len(text); i++ {
		if !m.first[text[i]] {
			continue
		}
		// When the first element matched one ASCII byte, the next byte
		// starts the second element, and its first byte is a cheap inline
		// reject that keeps matchAt off the common first-letter hits. A
		// non-ASCII first rune spans several bytes, so skip the shortcut.
		if needSecond && text[i] < utf8.RuneSelf && (i+1 >= len(text) || !m.second[text[i+1]]) {
			continue
		}
		if end, ok := m.matchAt(text, i); ok && m.boundaryHolds(text, i, end) {
			return true
		}
	}
	return false
}

// matchAt matches every element of the literal starting at byte offset i and
// returns the end offset.
func (m *matcher) matchAt(text string, i int) (int, bool) {
	pos := i
	for k := range m.elems {
		if pos >= len(text) {
			return 0, false
		}
		e := &m.elems[k]
		b := text[pos]
		if b < utf8.RuneSelf {
			if !e.accepts(b) {
				return 0, false
			}
			pos++
			continue
		}
		r, w := utf8.DecodeRuneInString(text[pos:])
		if r == utf8.RuneError && w <= 1 {
			return 0, false
		}
		if !e.acceptsRune(r) {
			return 0, false
		}
		pos += w
	}
	return pos, true
}

// boundaryHolds checks the \b conditions of a candidate occurrence
// text[start:end]. A byte at or above 0x80 is part of a non-ASCII rune and so
// is a non-word character, which is exactly how Go's \b classifies it.
func (m *matcher) boundaryHolds(text string, start, end int) bool {
	if m.before {
		prev := start > 0 && isWordByte(text[start-1])
		if prev == isWordByte(text[start]) {
			return false
		}
	}
	if m.after {
		next := end < len(text) && isWordByte(text[end])
		if isWordByte(text[end-1]) == next {
			return false
		}
	}
	return true
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// orbit returns the sorted simple-fold orbit of r, or just r when fold is
// false.
func orbit(r rune, fold bool) []rune {
	if !fold {
		return []rune{r}
	}
	out := []rune{r}
	for c := unicode.SimpleFold(r); c != r; c = unicode.SimpleFold(c) {
		out = append(out, c)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
