// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdocs

import (
	"strings"
	"unicode/utf8"
)

const maxContextBytes = 4096

// boundedContext caps searchable context at maxContextBytes, cutting on a rune
// boundary.
//
// The cap is a byte budget, but slicing at a byte offset can split a multi-byte
// rune and leave an invalid UTF-8 fragment (#5052). That is not cosmetic:
// searchhybrid.documentText checks utf8.ValidString and, on failure, rewrites
// the text through []rune, replacing the broken tail with U+FFFD — so the text
// that gets tokenized differs from the text that was stored, and that
// function's own comment says the canonicalization exists to keep the document
// hash stable. The fragment is also persisted and served through the API.
//
// Backing up to the last rune boundary keeps the result a true prefix of the
// input, which keeps re-bounding an already-bounded value a no-op.
func boundedContext(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxContextBytes {
		return value
	}
	cut := maxContextBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
