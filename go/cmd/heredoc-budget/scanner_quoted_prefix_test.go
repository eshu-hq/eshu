// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards P1-2 (codex review of PR #5890, filed against this
// branch's own F2 delimiter rewrite): parseDelim special-cased a delimiter
// word that STARTS with a quote in its own top-level branch, which returned
// immediately after the closing quote instead of continuing to scan the rest
// of the word -- unlike the unquoted-prefix branch's mid-word quote handling
// (`<<FOO'BAR'`, delimiter "FOOBAR"), which already continues correctly.
// `<<''E` returned delimiter "" (just the empty quoted pair) instead of real
// bash's "E", so the scanner then looked for a BLANK closing line instead of
// a literal "E" line: every intervening line -- including any real heredoc
// opener/body/closer text on it -- became inert body content of this
// phantom, wrongly-quoted heredoc, and closesHeredoc never got a chance to
// end it at the right place.
//
// Fixed by deleting the separate leading-quote branch entirely: a leading
// quote is now handled by the SAME '\'','"' case in the general word-scan
// loop that mid-word quotes already use, so both continue scanning after the
// quote closes.

// TestScanContent_EmptyQuotedPrefixConcatenatedDelimiter is the direct P1-2
// regression for an opener whose delimiter is an empty quoted pair directly
// followed by "E".
//
// Verified against real bash:
//
//	$ printf "cat <<''E\nbody line\nE\necho DONE-\$?\n" | bash
//	body line
//	DONE-0
func TestScanContent_EmptyQuotedPrefixConcatenatedDelimiter(t *testing.T) {
	body := strings.Repeat("z", 600) // over budget
	src := "cat <<''E\n" + body + "\nE\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the <<''E heredoc to close on the real delimiter \"E\" (not a phantom blank-line close), got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Unquoted {
		t.Fatalf("expected the quoted-prefix delimiter to disable expansion (Unquoted=false), got %+v", heredocs[0])
	}
	wantSize := len(body) + 1
	if heredocs[0].Size != wantSize {
		t.Fatalf("expected body size %d (just the 600-byte line), got %d -- a larger size means later lines were wrongly swallowed", wantSize, heredocs[0].Size)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_QuotedPrefixConcatenatedWithLiteralSuffix guards `<<'A'B`
// (quoted prefix directly followed by an unquoted suffix, no space).
//
// Verified against real bash:
//
//	$ printf "cat <<'A'B\nbody line\nAB\necho DONE-\$?\n" | bash
//	body line
//	DONE-0
func TestScanContent_QuotedPrefixConcatenatedWithLiteralSuffix(t *testing.T) {
	src := "cat <<'A'B\nbody line\nAB\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc closing on \"AB\", got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("body line\n") {
		t.Fatalf("expected body 'body line\\n' only, got size %d", heredocs[0].Size)
	}
}

// TestScanContent_OnlyQuotesDelimiterConcatenatesBothPairs is the mandated
// class-hunt probe "a delimiter that is only quotes": two adjacent empty
// quoted pairs back to back concatenate into one empty word, exactly like a
// single empty quoted pair alone, not a truncated one-pair read.
//
// Verified against real bash:
//
//	$ printf "cat <<''''\nbody\n\nfoo\necho after\n" | bash
//	body
//	foo: command not found
//	after
func TestScanContent_OnlyQuotesDelimiterConcatenatesBothPairs(t *testing.T) {
	src := "cat <<''''\nbody\n\nfoo\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc closing on the blank line, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("body\n") {
		t.Fatalf("expected body 'body\\n' only, got size %d", heredocs[0].Size)
	}
	if heredocs[0].Unquoted {
		t.Fatalf("expected an all-quotes delimiter to disable expansion (Unquoted=false), got %+v", heredocs[0])
	}
}

// TestScanContent_MixedQuoteStylesConcatenateIntoOneDelimiter is the mandated
// class-hunt probe "mixed quote styles" (`<<'A'"B"C`): a single-quoted
// segment, a double-quoted segment, and an unquoted suffix all concatenate
// into one word, "ABC".
//
// Verified against real bash:
//
//	$ printf "cat <<'A'\"B\"C\nbody\nABC\necho after\n" | bash
//	body
//	after
func TestScanContent_MixedQuoteStylesConcatenateIntoOneDelimiter(t *testing.T) {
	src := "cat <<'A'\"B\"C\nbody\nABC\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc closing on \"ABC\", got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("body\n") {
		t.Fatalf("expected body 'body\\n' only, got size %d", heredocs[0].Size)
	}
}

// TestScanContent_QuotedSegmentContainingSeparatorByteStaysOneWord is the
// mandated class-hunt probe "a quoted segment containing ... a separator
// character": a `;` (an isShellWordSeparator byte) inside a quoted segment of
// the delimiter must not end the word early -- it is literal quoted content,
// not a real statement separator, exactly like the space case already
// covered by TestScanContent_ConcatenatedQuotedSegmentInDelimiter's sibling
// probes.
//
// Verified against real bash:
//
//	$ printf "cat <<'A;B'\nbody\nA;B\necho after\n" | bash
//	body
//	after
func TestScanContent_QuotedSegmentContainingSeparatorByteStaysOneWord(t *testing.T) {
	src := "cat <<'A;B'\nbody\nA;B\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc closing on \"A;B\", got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("body\n") {
		t.Fatalf("expected body 'body\\n' only, got size %d", heredocs[0].Size)
	}
}

// TestScanContent_PhantomQuotedBodySwallowsLaterUnquotedMarginHeredoc is the
// SEVERE consequence codex identified for P1-2: before the fix, an opener
// whose delimiter was an empty quoted pair directly followed by "E"
// mis-parsed as an empty, quoted delimiter. Because the scanner then waited
// for a BLANK closing line instead of the real "E" line, every line in
// between -- including a completely unrelated, independent heredoc's own
// opener, body, and closer -- became inert body text folded into ONE phantom
// "quoted" heredoc. That phantom is measured against the FULL budget
// (Unquoted=false), not the stricter 384-byte unquoted-expansion margin, so a
// later heredoc whose own body sits in the 385-512 byte margin window (over
// the unquoted threshold, but comfortably under the full one) never gets
// independently measured against the threshold that actually applies to it --
// the margin bypasses entirely, and the phantom's inflated-but-still-under-512
// total reports no violation at all.
//
// Fixed (P1-2), that opener correctly closes on line 3 ("E"), so the
// REALDOC heredoc on lines 4-6 is scanned as its own, independent, unquoted heredoc
// and measured against the unquoted margin on its own merits.
func TestScanContent_PhantomQuotedBodySwallowsLaterUnquotedMarginHeredoc(t *testing.T) {
	marginBody := strings.Repeat("y", 390) // 391 with its own newline: > 384, <= 512
	src := strings.Join([]string{
		"cat <<''E",
		"first body line",
		"E",
		"cat <<REALDOC",
		marginBody,
		"REALDOC",
		"",
		"echo after",
	}, "\n")

	heredocs := ScanContent(src)

	if len(heredocs) != 2 {
		t.Fatalf("expected 2 independent heredocs (the \"E\" one and REALDOC), got %d: %+v -- fewer than 2 means REALDOC was folded into a phantom body instead of being independently scanned", len(heredocs), heredocs)
	}

	e := heredocs[0]
	if e.Line != 1 || e.Size != len("first body line\n") {
		t.Fatalf("expected the E heredoc to close right after \"first body line\" (Line=1, Size=%d), got %+v", len("first body line\n"), e)
	}

	realdoc := heredocs[1]
	if realdoc.Line != 4 {
		t.Fatalf("expected REALDOC's opener on line 4, got line %d (%+v)", realdoc.Line, realdoc)
	}
	if !realdoc.Unquoted {
		t.Fatalf("expected REALDOC (bare delimiter) to stay Unquoted=true, got %+v", realdoc)
	}
	wantSize := len(marginBody) + 1
	if realdoc.Size != wantSize {
		t.Fatalf("expected REALDOC body size %d, got %d", wantSize, realdoc.Size)
	}

	threshold := unquotedThreshold(defaultBudget)
	if realdoc.Size <= threshold {
		t.Fatalf("test setup bug: REALDOC size %d must exceed the unquoted margin %d to prove the bypass", realdoc.Size, threshold)
	}
	if realdoc.Size > defaultBudget {
		t.Fatalf("test setup bug: REALDOC size %d must stay under the full budget %d, or this stops proving a MARGIN-specific bypass", realdoc.Size, defaultBudget)
	}
	// The whole point: REALDOC is between the unquoted margin and the full
	// budget. Before the fix it was invisible (folded into an under-budget
	// phantom quoted body); after the fix it is its own heredoc and
	// ScanTree's Unquoted branch correctly flags it.
	violations := []Violation{}
	for _, h := range heredocs {
		th := defaultBudget
		if h.Unquoted {
			th = unquotedThreshold(defaultBudget)
		}
		if h.Size > th {
			violations = append(violations, Violation{Line: h.Line, Size: h.Size, Unquoted: h.Unquoted, Threshold: th})
		}
	}
	if len(violations) != 1 || violations[0].Line != 4 {
		t.Fatalf("expected exactly one margin violation, on REALDOC's line 4, got %+v", violations)
	}
}
