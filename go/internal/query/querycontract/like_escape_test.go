// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// TestEscapeLikeLiteral pins the one shared LIKE-literal escape (#10604,
// fix 05b): every production content-search bind pre-escapes the caller's
// pattern so %, _, and \ match literally under LIKE/ILIKE with the default
// escape. The transform is applied once, at bind time, to the raw pattern.
func TestEscapeLikeLiteral(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pattern string
		want    string
	}{
		{"empty", "", ""},
		{"plain", "handler", "handler"},
		{"percent", "100%coverage", `100\%coverage`},
		{"only wildcards", "%", `\%`},
		{"underscore", "han_ler", `han\_ler`},
		{"backslash", `a\b`, `a\\b`},
		{"trailing backslash", `trail\`, `trail\\`},
		{"mixed code", `fmt.Sprintf("%d", a_b)\n`, `fmt.Sprintf("\%d", a\_b)\\n`},
		{"non-ascii with wildcard", "naïveté_100%", `naïveté\_100\%`},
		{"escaped stays single", `\%`, `\\\%`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := EscapeLikeLiteral(tc.pattern); got != tc.want {
				t.Errorf("EscapeLikeLiteral(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}
