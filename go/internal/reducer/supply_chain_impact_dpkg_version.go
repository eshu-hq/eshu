// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"unicode"
)

func validDPKGVersion(value string) bool {
	parsed, ok := parseDPKGVersion(value)
	return ok && parsed.upstream != ""
}

type dpkgVersion struct {
	epoch    string
	upstream string
	revision string
}

func parseDPKGVersion(raw string) (dpkgVersion, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return dpkgVersion{}, false
	}
	epoch := ""
	if before, after, ok := strings.Cut(value, ":"); ok {
		if !isDecimalString(before) {
			return dpkgVersion{}, false
		}
		epoch = before
		value = after
	}
	upstream := value
	revision := "0"
	if idx := strings.LastIndex(value, "-"); idx >= 0 {
		upstream = value[:idx]
		revision = value[idx+1:]
	}
	if upstream == "" || revision == "" || !dpkgVersionCharsValid(upstream, true) ||
		!dpkgVersionCharsValid(revision, false) {
		return dpkgVersion{}, false
	}
	if !unicode.IsDigit(rune(upstream[0])) {
		return dpkgVersion{}, false
	}
	return dpkgVersion{epoch: epoch, upstream: upstream, revision: revision}, true
}

func compareDPKGVersion(a string, b string) (int, bool) {
	left, ok := parseDPKGVersion(a)
	if !ok {
		return 0, false
	}
	right, ok := parseDPKGVersion(b)
	if !ok {
		return 0, false
	}
	if cmp := compareRPMEpoch(left.epoch, right.epoch); cmp != 0 {
		return cmp, true
	}
	if cmp := compareDPKGPart(left.upstream, right.upstream); cmp != 0 {
		return cmp, true
	}
	return compareDPKGPart(left.revision, right.revision), true
}

func compareDPKGPart(a string, b string) int {
	left := dpkgPartScanner{value: a}
	right := dpkgPartScanner{value: b}
	for !left.done() || !right.done() {
		if cmp := compareDPKGNonDigits(&left, &right); cmp != 0 {
			return cmp
		}
		if cmp := compareDPKGDigits(left.nextDigits(), right.nextDigits()); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func compareDPKGNonDigits(left *dpkgPartScanner, right *dpkgPartScanner) int {
	for {
		l, lok := left.peekNonDigit()
		r, rok := right.peekNonDigit()
		lo := dpkgRuneOrder(l, lok)
		ro := dpkgRuneOrder(r, rok)
		if lo != ro {
			if lo < ro {
				return -1
			}
			return 1
		}
		if !lok && !rok {
			return 0
		}
		if lok {
			left.pos++
		}
		if rok {
			right.pos++
		}
	}
}

func compareDPKGDigits(left string, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

type dpkgPartScanner struct {
	value string
	pos   int
}

func (s *dpkgPartScanner) done() bool {
	return s.pos >= len(s.value)
}

func (s *dpkgPartScanner) peekNonDigit() (byte, bool) {
	if s.done() || isASCIIDigit(s.value[s.pos]) {
		return 0, false
	}
	return s.value[s.pos], true
}

func (s *dpkgPartScanner) nextDigits() string {
	start := s.pos
	for s.pos < len(s.value) && isASCIIDigit(s.value[s.pos]) {
		s.pos++
	}
	return s.value[start:s.pos]
}

func dpkgRuneOrder(value byte, ok bool) int {
	if !ok {
		return 0
	}
	if value == '~' {
		return -1
	}
	if isASCIIAlpha(value) {
		return int(value)
	}
	return int(value) + 256
}

func dpkgVersionCharsValid(value string, upstream bool) bool {
	for _, r := range value {
		switch {
		case unicode.IsDigit(r), unicode.IsLetter(r):
			continue
		case r == '.' || r == '+' || r == '~':
			continue
		case upstream && r == '-':
			continue
		default:
			return false
		}
	}
	return true
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}
