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
// The seven cases tagged "round-7 mutation kill" were added after #5927
// round-7 review found that round-7's own tightening -- the
// `-[a-zA-Z]*o[a-zA-Z]*[ \t]+pipefail\b` tail -- is strong enough on its own
// to survive removal of each OTHER guard this file's doc comment documents,
// with nothing in this matrix or trivyskipdirs_localscript_test.go noticing.
// The two cases tagged "round-8 mutation kill" close the same gap for two
// more untested guards round-8 review found: the '\n' stop in the negated
// class, and the mandatory whitespace immediately before the '-'-prefixed
// flag cluster. The four cases tagged "round-9 mutation kill" close it for
// four more: dropping '&' or '|' from the negated class each let an
// `-o`-shaped cluster in an unrelated command sharing the same `set`-prefixed
// line reach "pipefail" (fail-open, the same direction as the round-7/8
// cases above); dropping the second `[a-zA-Z]*` or the whole negated-class
// run each reject a legitimate invocation instead (fail-closed, the opposite
// direction -- `set -oe pipefail` and `set -e -o pipefail` both genuinely set
// pipefail). The one case tagged "round-10 mutation kill" closes the last
// one an independent element-by-element sweep found: dropping the '(?m)'
// flag makes production code fail closed on the real repo (proven directly
// against trivyskipdirs_test.go/trivyskipdirs_ci_test.go/
// trivyskipdirs_helper_test.go, which each read a real multi-line script or
// workflow), but "pipefail_on_later_line" is the only row in this matrix
// whose verdict CHANGES when '(?m)' is removed. The matrix has one other
// multi-line row, "newline_before_flag_cluster", but it wants false and
// stays false either way: `[^\n#;&|]*` already stops at the newline and
// blocks the cross-line reach regardless of multiline mode, so removing
// '(?m)' does not touch its verdict. Nothing in this matrix itself had
// noticed '(?m)' going missing until this row. It is the matrix's only
// killer of '(?m)', but -- because
// proving multi-line matching needs a genuine second-line invocation, and a
// genuine invocation needs every other required element too -- it also
// co-kills several mutations that already had a dedicated killer before this
// row existed (the '-', the first `[a-zA-Z]*`, the `[ \t]+` before
// "pipefail", and the indent/negated-class/second-`[a-zA-Z]*` quantifier
// tightenings; dropping the literal "set" itself does NOT co-kill this row --
// '\b' still matches at the start of line two either way). That overlap is
// expected, not a defect: it never gives an element ITS FIRST killer, only an
// additional one alongside a pre-existing named row, the same shape round-7's
// '#' and ';&|' fixes already have (each already reds both a matrix row and
// an end-to-end fixture in trivyskipdirs_localscript_test.go). Every one of
// these fourteen cases is a regex mutant proven,
// this session for the round-9 and round-10 additions and in the review
// round that added them for the rest, to flip trivyPipefailRE's verdict on
// its own named input when the named element is removed from
// trivyskipdirs.go's `trivyPipefailRE`; each has a true sibling already in
// this matrix (named in its own comment, or already present as its own row)
// proving the same mutation does not touch the legitimate forms it must
// still accept. Every element of the regex now has a killing row in this
// matrix.
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

		// round-7 mutation kill: dropping '#' from the negated class lets an
		// `-o`-shaped cluster INSIDE a trailing comment reach the "pipefail"
		// literal. Must NOT match -- the comment is not part of the `set`
		// command's own arguments.
		{"hash_before_flag_cluster", "set -x # -o pipefail", false},
		// True sibling: a genuine invocation followed by an unrelated
		// trailing comment must still match (proves the '#' stop does not
		// break real usage).
		{"hash_after_real_pipefail", "set -euo pipefail  # yes", true},

		// round-7 mutation kill: dropping ';&|' from the negated class lets
		// an `-o`-shaped cluster in an unrelated command AFTER a separator on
		// the same `set`-prefixed line reach "pipefail". Must NOT match --
		// mirrors the round-6 P3-1 hole, re-expressed against the new tail.
		{"semicolon_before_flag_cluster", "set -e; echo -o pipefail", false},
		// True sibling: "trailing_command" above already proves a real
		// invocation followed by an unrelated command after ';' still
		// matches.

		// round-7 mutation kill: dropping the '\b' after "set" lets a
		// longer word starting with "set" (settings, unset, reset) satisfy
		// the command-word check. Must NOT match.
		{"no_word_boundary_after_set", "settings -o pipefail", false},
		// True sibling: "o_only" above already proves the literal "set"
		// command still matches.

		// round-7 mutation kill: dropping the 'o' requirement from the flag
		// cluster lets ANY flag (not just an -o-bearing one) precede
		// "pipefail". Must NOT match -- "-e" alone never sets pipefail.
		{"flag_cluster_without_o", "set -e pipefail", false},
		// True sibling: "eo" above already proves a flag cluster that DOES
		// contain 'o' alongside other letters still matches.

		// round-7 mutation kill: loosening the `[ \t]+` before "pipefail" to
		// `[ \t]*` lets the flag cluster and "pipefail" run together with no
		// separating whitespace at all. Must NOT match -- a real bash flag
		// cluster and its value are always whitespace-separated words.
		{"no_space_before_pipefail", "set -opipefail", false},
		// True sibling: "o_only" above already proves the properly
		// space-separated form still matches.

		// round-7 mutation kill: dropping the '^[ \t]*' line anchor lets
		// "set ... pipefail" match anywhere inside a longer line, including
		// inside a quoted string that only MENTIONS the invocation rather
		// than performing it. Must NOT match.
		{"no_line_anchor", `echo "set -o pipefail"`, false},
		// True sibling: "indented" and "tab_indented" above already prove a
		// real `set` command anchored at (optionally indented) column 0
		// still matches.

		// round-7 mutation kill: dropping the trailing '\b' after "pipefail"
		// lets a longer word that merely starts with "pipefail" (e.g. a
		// hypothetical future flag name) satisfy the literal. Must NOT
		// match -- the word must be exactly "pipefail".
		{"no_trailing_word_boundary", "set -o pipefailoption", false},
		// True sibling: "o_only" above already proves the exact word
		// "pipefail" still matches.

		// round-8 mutation kill: dropping '\n' from the negated class lets
		// the scan cross a line break and treat a later line as if it were
		// still the `set` command's own argument list. Must NOT match -- a
		// `set` invocation with no pipefail flag on its own line, followed on
		// a LATER line by an unrelated command that happens to contain an
		// `-o`-shaped cluster next to "pipefail", is not the `set` command
		// establishing pipefail.
		{"newline_before_flag_cluster", "set -x\necho -o pipefail", false},
		// True sibling: every legitimate case above ("euo", "o_only", "eo",
		// "indented", "tab_indented", "trailing_command") is confined to one
		// physical line, so the '\n' stop never keeps a real invocation from
		// matching.

		// round-8 mutation kill: dropping the mandatory '[ \t]' immediately
		// before the '-'-prefixed flag cluster lets that cluster be found
		// anywhere after "set", including glued onto an unrelated hyphenated
		// token instead of starting its own shell word. Must NOT match --
		// "--foo-o" is one word, not `set`'s own "-o" flag.
		{"no_space_before_flag_cluster", "set -e --foo-o pipefail", false},
		// True sibling: "o_only" above already proves a flag cluster that IS
		// its own whitespace-separated word still matches.

		// round-9 mutation kill: dropping '&' from the negated class lets an
		// `-o`-shaped cluster inside an unrelated command joined by '&&' on
		// the same `set`-prefixed line reach "pipefail". Must NOT match --
		// proven against real bash: `set -e && echo -o pipefail` leaves
		// pipefail off (`set -o | grep pipefail` reports "off").
		{"ampersand_before_flag_cluster", "set -e && echo -o pipefail", false},
		// True sibling: "trailing_command" above already proves a real
		// invocation followed by an unrelated command on the same line still
		// matches.

		// round-9 mutation kill: dropping '|' from the negated class lets an
		// `-o`-shaped cluster inside an unrelated command joined by '|' on
		// the same `set`-prefixed line reach "pipefail". Must NOT match --
		// proven against real bash: `set -e | grep -o pipefail` leaves
		// pipefail off.
		{"pipe_before_flag_cluster", "set -e | grep -o pipefail", false},
		// True sibling: "trailing_command" above already proves a real
		// invocation followed by an unrelated command on the same line still
		// matches.

		// round-9 mutation kill: dropping the second '[a-zA-Z]*' lets a flag
		// cluster with a letter AFTER the 'o' (e.g. "-oe") fail to match.
		// Must match -- proven against real bash: `set -oe pipefail` sets
		// pipefail (`set -o | grep pipefail` reports "on"), and no true case
		// above puts a letter after the 'o'.
		{"o_then_letters", "set -oe pipefail", true},

		// round-9 mutation kill: dropping the whole negated-class run
		// '[^\n#;&|]*' leaves nothing between "set" and the '-'-prefixed flag
		// cluster, so a second, separate flag cluster preceding the one
		// bearing 'o' fails to match. Must match -- proven against real bash:
		// `set -e -o pipefail` sets pipefail, and no true case above has two
		// separate flag clusters.
		{"two_flag_clusters", "set -e -o pipefail", true},

		// round-10 mutation kill: dropping the '(?m)' flag makes '^' anchor
		// only the start of the WHOLE input instead of the start of every
		// line, so a real `set -euo pipefail` on any line after the first
		// stops matching. This is the only row in the matrix whose verdict
		// changes when '(?m)' is removed -- the other multi-line row,
		// "newline_before_flag_cluster", wants false and stays false either
		// way, because the negated class stops at the newline regardless of
		// multiline mode. Must match -- a script's `set -euo pipefail` is
		// ordinarily preceded by a shebang line, the same shape
		// checkScriptInvokesHelper reads from a real file, not a bare
		// one-line string.
		{"pipefail_on_later_line", "#!/bin/bash\nset -euo pipefail", true},
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
