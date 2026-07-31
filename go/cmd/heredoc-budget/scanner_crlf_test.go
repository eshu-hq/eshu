// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards P1-1 (codex review of PR #5890, filed against this
// branch's own F2 delimiter rewrite): on a CRLF-line-ending script, the
// broadened word scan in parseDelim records a bare delimiter's trailing '\r'
// as an ordinary word byte (real bash does the same -- '\r' is not a bash
// word separator, see isShellWordSeparator's doc comment), so an opener like
// "cat <<EOF\r\n" produced the parsed delimiter "EOF\r". closesHeredoc,
// however, already stripped a trailing '\r' from the CANDIDATE closing line
// before comparing (pre-existing CRLF tolerance). The two independently
// normalised sides then never matched -- "EOF\r" != "EOF" -- so the heredoc
// was dropped as unterminated and its body, however oversized, was never
// measured: a live #5074 hang risk sailing through the gate undetected.
//
// Verified against real bash (transcripts below and in the fix commit):
// bash's own heredoc delimiter for "cat <<EOF\r\n" genuinely includes the
// trailing '\r' (confirmed via the "wanted `EOF\r'" unterminated-heredoc
// warning when the closing line lacks a matching '\r'), and a
// consistently-CRLF script (both the opener line and the closing line
// carrying '\r') closes correctly in real bash purely because both sides
// happen to retain '\r' symmetrically -- not because bash special-cases it.
// This package's fix instead applies ONE shared stripTrailingCR
// normalisation to both the parsed delimiter (scanner_delim.go) and the
// candidate closing line (closesHeredoc, scanner.go), so a CRLF script
// closes correctly regardless of whether the '\r' arrived via a bare or a
// quoted delimiter, without depending on exact byte-for-byte symmetry.

func crlfJoin(lines ...string) string {
	return strings.Join(lines, "\r\n") + "\r\n"
}

// TestScanContent_CRLFBareDelimiterOverBudgetBodyMeasured is the direct P1-1
// regression: a CRLF script with a bare (unquoted) delimiter and an
// over-budget body must still be measured, not dropped as unterminated.
//
// Verified against real bash:
//
//	$ printf 'cat <<EOF\r\nhello\r\nEOF\r\necho DONE-$?\r\n' | bash
//	hello
//	DONE-0
//
// (exit 0, no "unterminated" warning -- the CRLF heredoc closes normally).
func TestScanContent_CRLFBareDelimiterOverBudgetBodyMeasured(t *testing.T) {
	body := strings.Repeat("z", 600) // over budget once measured
	src := crlfJoin("cat <<EOF", body, "EOF", "echo after")

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the CRLF EOF heredoc to be measured (not dropped as unterminated), got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 1 {
		t.Fatalf("expected the opener on line 1, got line %d", heredocs[0].Line)
	}
	wantSize := len(body) + 1 + 1 // body content + its own '\r' + the split-removed '\n'
	if heredocs[0].Size != wantSize {
		t.Fatalf("expected body size %d, got %d", wantSize, heredocs[0].Size)
	}
	if !heredocs[0].Unquoted {
		t.Fatalf("expected a bare CRLF delimiter to stay Unquoted=true, got %+v", heredocs[0])
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CRLFTabStripDelimiterOverBudgetBodyMeasured is the same P1-1
// gap combined with `<<-` tab-stripping (mandated class-hunt probe: "CRLF
// combined with `<<-`").
//
// Verified against real bash:
//
//	$ printf 'cat <<-EOF\r\n\tbody\r\n\tEOF\r\necho after\r\n' | bash
//	body
//	after
//
// (exit 0, no warning).
func TestScanContent_CRLFTabStripDelimiterOverBudgetBodyMeasured(t *testing.T) {
	body := "\t" + strings.Repeat("z", 600)
	src := crlfJoin("cat <<-EOF", body, "\tEOF", "echo after")

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the CRLF <<-EOF heredoc to be measured (not dropped as unterminated), got %d: %+v", len(heredocs), heredocs)
	}
	wantSize := len(body) + 1 + 1
	if heredocs[0].Size != wantSize {
		t.Fatalf("expected body size %d, got %d", wantSize, heredocs[0].Size)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CRLFQuotedDelimiterOverBudgetBodyMeasured guards the quoted
// side of P1-1: a quoted delimiter (`<<'EOF'`) followed immediately by the
// physical line's trailing '\r' must ALSO close correctly, since fixing P1-2
// (below) makes the quoted branch continue scanning past the closing quote
// and pick up that trailing '\r' as part of the same word -- exactly like any
// other quoted-prefix concatenation.
//
// Verified against real bash:
//
//	$ printf "cat <<'EOF'\r\nhello\r\nEOF\r\necho DONE-\$?\r\n" | bash
//	hello
//	DONE-0
func TestScanContent_CRLFQuotedDelimiterOverBudgetBodyMeasured(t *testing.T) {
	body := strings.Repeat("z", 600)
	src := crlfJoin("cat <<'EOF'", body, "EOF", "echo after")

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the CRLF quoted EOF heredoc to be measured, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Unquoted {
		t.Fatalf("expected a quoted delimiter to disable expansion (Unquoted=false), got %+v", heredocs[0])
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CRLFDanglingBackslashEscapesCRNotContinuation is a
// mandated class-hunt probe ("CRLF combined with a line continuation"). A
// trailing backslash is only a real bash line continuation when it is
// IMMEDIATELY followed by '\n' -- on a CRLF line the byte immediately after
// the backslash is '\r', not '\n', so real bash does NOT splice the next
// physical line on. Instead the backslash is an ordinary mid-word escape of
// the '\r' itself, producing a delimiter that literally ends in an escaped
// '\r' and quoted=true; since the real closing line never supplies a
// matching escaped '\r', the heredoc is unterminated in real bash too.
//
// Verified against real bash:
//
//	$ printf 'cat <<EOF\\\r\nSECOND\r\nbody\r\nEOFSECOND\r\necho after\r\n' | bash
//	...: warning: here-document at line 1 delimited by end-of-file (wanted `EOF<CR>')
//
// (the whole rest of the script is swallowed as the heredoc's body; "after"
// never runs as a separate command). This scanner must match that verdict:
// zero heredocs, not a phantom continuation-fused "EOFSECOND" delimiter.
func TestScanContent_CRLFDanglingBackslashEscapesCRNotContinuation(t *testing.T) {
	src := crlfJoin(`cat <<EOF\`, "SECOND", "body", "EOFSECOND", "echo after")

	heredocs := ScanContent(src)

	if len(heredocs) != 0 {
		t.Fatalf("expected zero heredocs (CRLF backslash escapes '\\r', it is not a continuation, matching real bash's own unterminated verdict), got %d: %+v", len(heredocs), heredocs)
	}
}
