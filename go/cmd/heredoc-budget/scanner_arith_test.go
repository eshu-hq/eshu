// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards the F1 fail-open (2026-07 hardening review): `$((` opens
// bash arithmetic evaluation, not a command substitution, and `<<` inside it
// is the arithmetic SHIFT operator, never a heredoc opener. The old scanner
// pushed a single frameSubst frame for the FIRST `(` of `$((` (indistinguishable
// from plain `$(cmd)`) and had no model at all for the second `(`, so nothing
// suppressed heredoc-opener detection inside the arithmetic body. The existing
// numeric-first-delimiter rule (parseDelim rejecting `<<123`) only blocks a
// *numeric* shift operand; an identifier-shaped one (`x=$(( flags <<
// shiftamount ))`) sailed through parseDelim as a normal-looking delimiter
// name, phantom-opening an "unterminated" heredoc that swallows every real
// heredoc later in the file -- 0 detected, exit 0, the live-hang fail-open
// this gate exists to catch (#5074/#5085/#5079).
//
// Verified against real bash (5.3.15, this machine):
//
//	x=$(( flags << shiftamount ))
//	cat <<REALBIG
//	<601-byte body>
//	REALBIG
//	echo done
//
// exits 0, printing the 601-byte body then "done" -- exactly one real
// heredoc. `ScanContent` on the same source must report exactly one Heredoc
// (REALBIG, over budget), not zero.

func TestScanContent_ArithmeticShiftOperandNotMisreadAsHeredocDelimiter(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n" // 601 bytes, over budget
	src := "x=$(( flags << shiftamount ))\ncat <<REALBIG\n" + body + "REALBIG\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALBIG heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_ArithmeticShiftStillRejectedAsHeredoc is the pre-existing
// numeric-operand regression guard (TestScanContent_WhitespaceBeforeDelimHandled
// in scanner_test.go already covers `x=$(( a << 2 ))`); this table extends it
// with the identifier-operand and no-space forms that the old parseDelim-only
// mitigation missed, plus a nested-parens body to prove frameArith's
// paren-depth tracking finds the real closing `))` instead of miscounting.
func TestScanContent_ArithmeticShiftStillRejectedAsHeredoc(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"identifier_operand", "x=$(( flags << shiftamount ))"},
		{"numeric_operand_no_space", "x=$((a<<2))"},
		{"nested_grouping_parens", "x=$(( (flags << 1) + offset ))"},
		{"double_left_shift_reassign", "count=$(( count << count ))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h := ScanContent(tt.expr + "\n"); len(h) != 0 {
				t.Fatalf("arithmetic shift %q wrongly parsed as a heredoc: %+v", tt.expr, h)
			}
		})
	}
}

// TestScanContent_ArithmeticDoesNotSuppressComments guards the flip side: a
// literal '#' inside `$(( ))` is NOT a bash comment (arithmetic has no
// comment syntax -- verified against real bash: `(( 1 <#comment\n2 ))`
// reports an arithmetic syntax error citing the literal "#comment" text, not
// a stripped comment), so findAllOpeners must not apply its word-start
// comment rule while top-of-stack is frameArith. This guards against an
// over-broad fix that suppresses '#' handling everywhere once frameArith
// exists, which would silently swallow a real heredoc opener that follows a
// literal '#' inside an arithmetic expression on the same line.
func TestScanContent_ArithmeticDoesNotSuppressComments(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	// The '#' sits inside $(( )); after it closes, a real heredoc opener
	// follows on the same line and must still be found.
	src := "x=$(( 1 + 2 )) #<<NOTREAL\ncat <<REALEOF\n" + body + "REALEOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected only the real REALEOF heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_NestedCommandSubstitutionInsideArithmeticStillRecognized
// guards that command substitution still works INSIDE arithmetic (real bash
// allows `$(cmd)` inside `$(( ))`), and that a real heredoc nested inside
// that inner substitution is still detected -- frameArith must not blind the
// scanner to a nested frameSubst.
func TestScanContent_NestedCommandSubstitutionInsideArithmeticStillRecognized(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "x=$(( $(cat <<EOF\n" + body + "EOF\n) + 1 ))\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the heredoc nested inside $(...) inside $(( )) to be detected, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}
