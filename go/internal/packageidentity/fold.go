// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "strings"

// LowerASCII returns value with only the ASCII letters A-Z mapped to a-z. Every
// other byte, including each byte of a non-ASCII letter, is kept as written.
//
// Package identity folds case with this function, never with strings.ToLower:
// Unicode case mapping lowers U+0130 (capital I with dot above) to "i" and
// U+212A (Kelvin sign) to "k", which would give a lookalike name such as
// "Ser\u0130log" the key of the real "Serilog" package (#9584). No registry Eshu
// reads defines a Unicode case fold for package names: PyPI, Go module paths,
// and crates.io accept only ASCII names, npm requires URL-safe names, and
// NuGet IDs may not hold characters that are not valid in a URL. ASCII-only
// folding is byte-identical to strings.ToLower for every ASCII input.
func LowerASCII(value string) string {
	first := -1
	for i := 0; i < len(value); i++ {
		if c := value[i]; 'A' <= c && c <= 'Z' {
			first = i
			break
		}
	}
	if first < 0 {
		return value
	}
	var lowered strings.Builder
	lowered.Grow(len(value))
	lowered.WriteString(value[:first])
	for i := first; i < len(value); i++ {
		c := value[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		lowered.WriteByte(c)
	}
	return lowered.String()
}

// ContainsNonASCII reports whether value holds any byte outside ASCII. Callers
// use it to count package names whose non-ASCII letters LowerASCII keeps as
// written, so an operator can see how many rows the #9584 rule affects.
func ContainsNonASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 0x80 {
			return true
		}
	}
	return false
}
