// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

// logical describes one logical properties line: a physical line plus the
// lines a trailing backslash joins to it.
type logical struct {
	end      int  // offset of the end of the last physical line
	next     int  // offset where the next logical line starts
	physical int  // physical lines consumed
	dangling bool // a continuation backslash with no line after it
}

// isKeyEnd reports whether b ends a property key.
func isKeyEnd(b byte) bool {
	return b == ' ' || b == '\t' || b == '\f' || b == '=' || b == ':'
}

func isBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\f' }

// lineEnd returns the offset of the first line terminator at or after pos, or
// len(source).
func lineEnd(source []byte, pos int) int {
	for pos < len(source) && source[pos] != '\n' && source[pos] != '\r' {
		pos++
	}
	return pos
}

// nextLine returns the offset after the terminator at eol. CR LF counts once.
func nextLine(source []byte, eol int) int {
	if eol >= len(source) {
		return len(source)
	}
	if source[eol] == '\r' && eol+1 < len(source) && source[eol+1] == '\n' {
		return eol + 2
	}
	return eol + 1
}

// firstNonBlank returns the offset of the first non-blank byte on the line
// that starts at pos, or the line end.
func firstNonBlank(source []byte, pos int) int {
	for pos < len(source) && isBlank(source[pos]) {
		pos++
	}
	return pos
}

// endsInContinuation reports whether the physical line source[from:eol] ends in
// an odd run of backslashes, which joins it to the next line.
func endsInContinuation(source []byte, from, eol int) bool {
	run := 0
	for i := eol - 1; i >= from && source[i] == '\\'; i-- {
		run++
	}
	return run%2 == 1
}

// logicalLine finds the end of the logical line that starts at pos.
func logicalLine(source []byte, pos int) logical {
	l := logical{}
	for {
		eol := lineEnd(source, pos)
		l.physical++
		l.end = eol
		next := nextLine(source, eol)
		if !endsInContinuation(source, pos, eol) {
			l.next = next
			return l
		}
		if next >= len(source) {
			l.next, l.dangling = len(source), true
			return l
		}
		pos = next
	}
}

// logicalValue assembles the value of a logical line that starts after the key
// at keyEnd and ends at end. It drops the separator, joins continuation lines
// without their leading blanks, and applies the escapes the grammar knows (a
// backslash before a colon, equals sign, backslash, hash, bang or space). It
// returns false for any other escape and when the value passes MaxValueBytes.
func logicalValue(source []byte, keyEnd, end int) ([]byte, bool) {
	pos := firstNonBlank(source, keyEnd)
	if pos < end && (source[pos] == '=' || source[pos] == ':') {
		pos = firstNonBlank(source, pos+1)
	}
	value := make([]byte, 0, 128)
	for {
		eol := lineEnd(source, pos)
		segmentEnd := eol
		continued := endsInContinuation(source, pos, eol)
		if continued {
			segmentEnd--
		}
		for i := pos; i < segmentEnd; i++ {
			b := source[i]
			if b == '\\' {
				i++
				switch source[i] {
				case ':', '=', '\\', '#', '!', ' ':
					b = source[i]
				default:
					return nil, false
				}
			}
			if len(value) == MaxValueBytes {
				return nil, false
			}
			value = append(value, b)
		}
		if !continued || eol >= end {
			return value, true
		}
		pos = firstNonBlank(source, nextLine(source, eol))
	}
}
