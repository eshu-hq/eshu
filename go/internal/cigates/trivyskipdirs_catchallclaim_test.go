// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"
)

// This file was split out of trivyskipdirs_test.go to keep that file under
// the package's 500-line-per-file limit (see go/internal/cigates/AGENTS.md),
// the same reason trivyskipdirs_localscript_test.go and
// trivyskipdirs_ci_test.go were split out earlier.

// containsCatchAllClaim reports whether s -- normalized to lowercase with
// hyphens and spaces removed -- contains "catchall". It closes a spelling-
// variant hole a literal strings.Contains(got, "catch-all") check missed
// (#5927 round-7 review P3-1): a self-contradicting message spelled
// "catchall" or "catch all" passed that literal check silently, because
// neither spelling contains the hyphenated substring "catch-all". The
// normalization necessarily also flags truthful phrasing like "not a
// catch-all" -- a known, accepted fail-closed trap (the same shape
// TestTrivySkipDirsParity_SpecsFileEmptyNormalizingEntryFailsLoudly's own
// doc comment already warns about), not a new defect: a plain substring
// check cannot distinguish a claim from its negation, and failing loudly on
// an ambiguous message is safer than silently accepting one that might be a
// real regression.
func containsCatchAllClaim(s string) bool {
	normalized := strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(s))
	return strings.Contains(normalized, "catchall")
}

// TestContainsCatchAllClaim pins containsCatchAllClaim's normalization
// directly: the spelling variants a hyphen-only check missed (#5927 round-7
// review P3-1), the real hyphenated spelling it always caught, a mixed-case
// input proving the lowercasing half of the normalization, an unrelated
// message that must not false-positive, and the truthful "not a catch-all"
// phrasing documented above as an accepted, intentional false-positive
// rather than an oversight.
func TestContainsCatchAllClaim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		s    string
		want bool
	}{
		{"hyphenated", "it is a catch-all that disables the scan", true},
		{"no_hyphen", "it is a catchall that disables the scan", true},
		{"spaced", "it is a catch all that disables the scan", true},
		{"mixed_case", "it is a Catch-All that disables the scan", true},
		{"unrelated", "the entry is dead weight, not a problem", false},
		// Documented trap: a substring check cannot tell a claim from its
		// negation, so truthful "not a catch-all" phrasing also reports
		// true. See containsCatchAllClaim's doc comment.
		{"truthful_negation", "it is dead weight, not a catch-all", true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := containsCatchAllClaim(c.s); got != c.want {
				t.Errorf("containsCatchAllClaim(%q) = %v, want %v", c.s, got, c.want)
			}
		})
	}
}
