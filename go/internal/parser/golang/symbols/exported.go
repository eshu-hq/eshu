// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"unicode"
	"unicode/utf8"
)

// IdentifierIsExported reports whether name begins with an upper-case rune,
// matching Go's export rule for package-level identifiers.
func IdentifierIsExported(name string) bool {
	first, _ := utf8.DecodeRuneInString(name)
	return first != utf8.RuneError && unicode.IsUpper(first)
}
