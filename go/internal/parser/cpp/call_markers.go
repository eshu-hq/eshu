// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"strings"
	"unicode"
)

func cppHasCallMarker(text string, marker string) bool {
	return cppCallMarkerIndex(text, marker) >= 0
}

func cppCallMarkerIndex(text string, marker string) int {
	start := 0
	for {
		index := strings.Index(text[start:], marker)
		if index < 0 {
			return -1
		}
		index += start
		after := index + len(marker)
		if cppIdentifierBoundaryBefore(text, index) && cppNextNonSpaceIsOpenParen(text, after) {
			return index
		}
		start = after
	}
}

func cppNextNonSpaceIsOpenParen(text string, index int) bool {
	for _, r := range text[index:] {
		if unicode.IsSpace(r) {
			continue
		}
		return r == '('
	}
	return false
}

func cppIdentifierBoundaryBefore(text string, index int) bool {
	if index == 0 {
		return true
	}
	before := rune(text[index-1])
	return !unicode.IsLetter(before) && !unicode.IsDigit(before) && before != '_'
}
