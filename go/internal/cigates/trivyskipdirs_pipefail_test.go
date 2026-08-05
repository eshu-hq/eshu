// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "testing"

// TestTrivyPipefailRE_AcceptanceMatrix pins #5925/#5927 round-7 review F2
// directly against the regex, not only through the full drift-check path
// trivyskipdirs_localscript_test.go exercises. `set +o pipefail` DISABLES
// pipefail (bash's `+o` turns an option OFF, `-o` turns it ON) but used to
// read as establishing it, because the prior pattern required only the bare
// word "pipefail" somewhere after "set" with no check on which sign
// introduced it.
//
// package cigates (not cigates_test) so this test can reference the
// unexported trivyPipefailRE directly, the same internal-test-package
// pattern trivyskipdirs_csv_test.go already uses in this directory.
func TestTrivyPipefailRE_AcceptanceMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want bool
	}{
		// Legitimate forms that must still match.
		{"euo", "set -euo pipefail", true},
		{"o_only", "set -o pipefail", true},
		{"eo", "set -eo pipefail", true},
		{"indented", "  set -euo pipefail", true},
		{"tab_indented", "\tset -euo pipefail", true},
		{"trailing_command", `set -euo pipefail; echo hi`, true},
		// The regression this round closes: '+o' DISABLES pipefail and must
		// NOT match.
		{"plus_o", "set +o pipefail", false},
		{"plus_o_indented", "  set +o pipefail", false},
		{"plus_o_trailing_command", `set +o pipefail; echo hi`, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := trivyPipefailRE.MatchString(c.line); got != c.want {
				t.Errorf("trivyPipefailRE.MatchString(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}
