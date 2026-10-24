// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateForLogBoundsAndKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"short":          "2024-11-05",
		"exactly at cap": strings.Repeat("a", maxLoggedVersionBytes),
		"long ascii":     strings.Repeat("a", 500),
		// 3-byte runes put the cap in the middle of a rune.
		"long multibyte": strings.Repeat("é", 200) + strings.Repeat("世", 200),
		"split rune cap": strings.Repeat("a", maxLoggedVersionBytes-1) + "世",
	}
	for name, in := range cases {
		out := truncateForLog(in)
		if !utf8.ValidString(out) {
			t.Errorf("%s: output is not valid UTF-8: %q", name, out)
		}
		if len(out) > maxLoggedVersionBytes+len("...") {
			t.Errorf("%s: output is %d bytes, over the cap", name, len(out))
		}
		if len(in) <= maxLoggedVersionBytes && out != in {
			t.Errorf("%s: a value within the cap must pass through, got %q", name, out)
		}
	}
}
