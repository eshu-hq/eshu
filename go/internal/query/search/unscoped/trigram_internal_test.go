// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "testing"

// The trigram test mirrors pg_trgm extraction for an ILIKE pattern: only a
// run of at least three alphanumeric characters yields a trigram the bitmap
// can require. Wildcards, escapes and punctuation break a run.
func TestPatternHasTrigram(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern string
		want    bool
	}{
		{"", false},
		{"a", false},
		{"ab", false},
		{"db_", false},
		{"a_b", false},
		{"%", false},
		{"x%yz", false},
		{"ab-cd", false},
		{"a\\bc", false}, // single backslash: ILIKE reads \b as literal b
		// and the planner still extracts trigrams (replica EXPLAIN plans a
		// Bitmap Index Scan for this exact pattern), but the check breaks
		// on the escape — the pinned conservative divergence: a usable
		// tail is skipped, never a useless one run (see trigram.go)
		{"a1_", false},
		{"abc", true},
		{"decode", true},
		{"ab-cde", true},
		{"_abc_", true},
		{"ééé", true},
		{"x9y8z", true},
	}
	for _, tc := range cases {
		if got := patternHasTrigram(tc.pattern); got != tc.want {
			t.Errorf("patternHasTrigram(%q) = %v, want %v", tc.pattern, got, tc.want)
		}
	}
}
