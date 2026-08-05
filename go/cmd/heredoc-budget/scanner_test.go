// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- scanner tests -----------------------------------------------------

func TestScanContent_OverBudgetHeredocFlagged(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n" // 601 bytes, over the 512 budget
	src := "#!/usr/bin/env bash\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected body size > %d, got %d", defaultBudget, heredocs[0].Size)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected opener on line 2, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_CommentOpenerDoesNotHideRealHeredoc guards the #5074 review
// false-negative: a `<<IDENT` inside a full-line comment must not phantom-open
// the scanner and desync it so a later real oversized heredoc is missed (the
// fail-open case — the gate would pass while make pre-pr still hangs).
func TestScanContent_CommentOpenerDoesNotHideRealHeredoc(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n" // 601 bytes, over budget
	// A comment mentioning `<<DONE` precedes a real oversized heredoc.
	src := "#!/usr/bin/env bash\n# see the <<DONE marker used elsewhere\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real heredoc to be detected despite the comment opener, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected body size > %d, got %d", defaultBudget, heredocs[0].Size)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_TrailingCommentOpenerDoesNotHideRealHeredoc guards a P1
// fail-open in the fix above: only a FULL-line comment was recognized, so a
// comment that trails real code on the same line (e.g. "echo x # <<EOF") was
// not treated as a comment at all -- the `<<EOF` inside it phantom-opened the
// scanner exactly like the full-line case, and since no later line in the
// script is literally "EOF" (the real heredoc uses "REALEOF"), the opener
// with no matching close is silently dropped (per ScanContent's documented
// behavior for malformed input) -- swallowing the real over-budget heredoc
// entirely: 0 detected, exit 0. Verified against real /bin/bash: `echo x #
// <<EOF` prints "x" and opens no heredoc at all; a following `cat <<REALEOF`
// is a completely independent, real heredoc.
func TestScanContent_TrailingCommentOpenerDoesNotHideRealHeredoc(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n" // 601 bytes, over budget
	src := "#!/usr/bin/env bash\n" +
		"echo x # <<EOF\n" +
		"cat <<REALEOF\n" + body + "REALEOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real REALEOF heredoc to be detected despite the trailing comment opener, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected body size > %d, got %d", defaultBudget, heredocs[0].Size)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_WhitespaceBeforeDelimHandled guards the #5074 review P1
// fail-open: bash accepts blanks between `<<`/`<<-` and the delimiter
// (`cat << EOF`, `cat <<- 'EOF'`), and such a >512B heredoc must still be
// detected or it slips past the gate and hangs make pre-pr.
func TestScanContent_WhitespaceBeforeDelimHandled(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n" // 601 bytes, over budget
	for _, opener := range []string{"cat << EOF", "cat <<  EOF", "cat <<- 'EOF'", "cat << \"EOF\""} {
		src := opener + "\n" + body + "EOF\n"
		heredocs := ScanContent(src)
		if len(heredocs) != 1 || heredocs[0].Size <= defaultBudget {
			t.Fatalf("opener %q: expected one over-budget heredoc, got %+v", opener, heredocs)
		}
	}
	// An arithmetic left-shift must NOT be mistaken for a heredoc opener.
	if h := ScanContent("x=$(( a << 2 ))\n"); len(h) != 0 {
		t.Fatalf("arithmetic `<< 2` wrongly parsed as a heredoc: %+v", h)
	}
}

func TestScanContent_UnderBudgetHeredocNotFlagged(t *testing.T) {
	body := strings.Repeat("a", 100) + "\n" // 101 bytes, under the 512 budget
	src := "cat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size >= defaultBudget {
		t.Fatalf("expected body size < %d, got %d", defaultBudget, heredocs[0].Size)
	}
}

func TestScanContent_HereStringIgnored(t *testing.T) {
	src := "grep foo <<< \"$var\"\ncat <<EOF\nshort body\nEOF\n"

	heredocs := ScanContent(src)

	// Only the real heredoc should be detected; the here-string must never
	// be mistaken for a heredoc opener.
	if len(heredocs) != 1 {
		t.Fatalf("expected here-string to be ignored (1 real heredoc), got %d: %+v", len(heredocs), heredocs)
	}
}

func TestScanContent_TabStrippedCloseHandled(t *testing.T) {
	body := strings.Repeat("b", 600) + "\n"
	src := "cat <<-EOF\n" + body + "\t\tEOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

func TestScanContent_QuotedDelimHandled(t *testing.T) {
	body := strings.Repeat("c", 50) + "\n"
	src := "cat <<'EOF'\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc for quoted delimiter, got %d: %+v", len(heredocs), heredocs)
	}
}

func TestScanContent_DoubleQuotedDelimHandled(t *testing.T) {
	body := strings.Repeat("c", 50) + "\n"
	src := "cat <<\"EOF\"\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc for double-quoted delimiter, got %d: %+v", len(heredocs), heredocs)
	}
}

func TestScanContent_TwoHeredocsBothCounted(t *testing.T) {
	bodyA := strings.Repeat("a", 600) + "\n"
	bodyB := strings.Repeat("b", 700) + "\n"
	src := "cat <<A\n" + bodyA + "A\necho middle\ncat <<B\n" + bodyB + "B\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 2 {
		t.Fatalf("expected 2 heredocs, got %d: %+v", len(heredocs), heredocs)
	}
	for i, h := range heredocs {
		if h.Size <= defaultBudget {
			t.Fatalf("heredoc %d expected over budget, got %d", i, h.Size)
		}
	}
}

func TestScanContent_DelimWordInsideOtherBodyNotMisclosed(t *testing.T) {
	// The body of the EOF heredoc contains a line that is exactly "INNER",
	// which is not the current open delimiter, so it must not close the
	// heredoc early or otherwise corrupt the body size.
	src := "cat <<EOF\n" + "before\n" + "INNER\n" + strings.Repeat("x", 600) + "\n" + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc (a mis-close on INNER would produce a different count), got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body spanning past the INNER line, got %d", heredocs[0].Size)
	}
}

func TestScanContent_UnterminatedHeredocDropped(t *testing.T) {
	// A heredoc opener with no matching closing line is malformed; it must
	// not be reported (nothing to flag) and must not corrupt later parsing.
	src := "cat <<EOF\nbody with no closer\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 0 {
		t.Fatalf("expected unterminated heredoc to be dropped, got %d: %+v", len(heredocs), heredocs)
	}
}

// TestScanContent_UnquotedNearBudgetHeredocMarksMargin guards #5085: an
// UNQUOTED heredoc (<<DELIM, not <<'DELIM') can expand at runtime via
// ${var}/$(cmd) substitution even though its literal source is under the
// raw budget. ScanContent must record Unquoted so ScanTree can apply a
// stricter effective threshold; a quoted heredoc of the same size never
// expands and must not be marked.
func TestScanContent_UnquotedNearBudgetHeredocMarksMargin(t *testing.T) {
	body := strings.Repeat("a", 449) + "\n" // 450 bytes: under the 512 raw budget

	unquoted := ScanContent("cat <<EOF\n" + body + "EOF\n")
	if len(unquoted) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(unquoted), unquoted)
	}
	if unquoted[0].Size != 450 {
		t.Fatalf("expected body size 450, got %d", unquoted[0].Size)
	}
	if !unquoted[0].Unquoted {
		t.Fatalf("expected bare <<EOF delimiter to be marked Unquoted, got %+v", unquoted[0])
	}

	quoted := ScanContent("cat <<'EOF'\n" + body + "EOF\n")
	if len(quoted) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(quoted), quoted)
	}
	if quoted[0].Unquoted {
		t.Fatalf("expected quoted <<'EOF'> delimiter to NOT be marked Unquoted, got %+v", quoted[0])
	}
}

// TestScanContent_HeredocMarkerInsideStringLiteralNotPhantomOpened guards
// #5079: a `<<IDENT` written inside a double-quoted string literal (e.g.
// echo "a <<X b") must not phantom-open the scanner. A phantom open desyncs
// the state machine so a REAL heredoc later in the file is silently dropped
// -- the dangerous fail-open case this gate exists to prevent.
func TestScanContent_HeredocMarkerInsideStringLiteralNotPhantomOpened(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n" // 601 bytes, over budget
	src := `echo "a <<X b"` + "\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real heredoc to be detected despite the string-literal marker, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_HeredocMarkerInsideSingleQuotedStringNotPhantomOpened is
// the single-quote variant of the same guard.
func TestScanContent_HeredocMarkerInsideSingleQuotedStringNotPhantomOpened(t *testing.T) {
	body := strings.Repeat("a", 600) + "\n"
	src := "echo 'a <<X b'\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real heredoc to be detected despite the string-literal marker, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_QuotedArgBeforeOpenerOnSameLineStillDetected is the clean
// case for the string-literal guard above: an ordinary quoted argument with
// no heredoc marker inside it must not interfere with a real opener later on
// the same line.
func TestScanContent_QuotedArgBeforeOpenerOnSameLineStillDetected(t *testing.T) {
	body := strings.Repeat("a", 50) + "\n"
	src := `echo "hello world" <<EOF` + "\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
}

// TestScanContent_TwoOpenersOnOneLineBothMeasured guards #5079: `cmd <<A
// <<B` opens two heredocs on the same source line. Bash reads their bodies
// back to back, in order, immediately following the command line. A scanner
// that only tracks the first opener silently drops the second body -- a
// real over-budget heredoc there would never be measured (fail-open).
func TestScanContent_TwoOpenersOnOneLineBothMeasured(t *testing.T) {
	bodyA := strings.Repeat("a", 600) + "\n" // over budget
	bodyB := strings.Repeat("b", 700) + "\n" // over budget
	src := "cat <<A <<B\n" + bodyA + "A\n" + bodyB + "B\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 2 {
		t.Fatalf("expected both heredoc openers on the line to be measured, got %d: %+v", len(heredocs), heredocs)
	}
	for i, h := range heredocs {
		if h.Line != 1 {
			t.Fatalf("heredoc %d: expected opener line 1 (both openers share the command line), got %d", i, h.Line)
		}
		if h.Size <= defaultBudget {
			t.Fatalf("heredoc %d: expected over-budget body, got %d", i, h.Size)
		}
	}
	if heredocs[0].Size == heredocs[1].Size {
		t.Fatalf("sanity: bodies should differ in size (600 vs 700 'a'/'b' chars), got equal sizes %d", heredocs[0].Size)
	}
}

// TestScanContent_PerOpenerQuotedSurvivesQueue guards #5079's multi-opener
// sub-case against a fail-open the existing TestScanContent_TwoOpenersOnOneLineBothMeasured
// cannot see: it only exercises two BARE openers, so it cannot tell whether a
// DEQUEUED opener (ScanContent's `pending` -> `current` promotion in
// scanner.go) keeps ITS OWN quoted-ness or wrongly inherits the
// already-closed opener's. A body whose delimiter was bare (`<<DELIM`) must
// be measured against the stricter unquotedThreshold margin (#5085); a
// dequeued opener that inherits the wrong quoted flag either hides a real
// margin violation (quoted-leaks-onto-bare) or wrongly tightens a heredoc
// that bash never expands (bare-leaks-onto-quoted). Each case below pairs
// openers of different quotedness so a leak in either direction, and across
// more than one dequeue in the three-opener case, flips an assertion.
func TestScanContent_PerOpenerQuotedSurvivesQueue(t *testing.T) {
	t.Run("quoted_then_bare_second_opener_measured_unquoted", func(t *testing.T) {
		bodyA := strings.Repeat("a", 50) + "\n"
		bodyB := strings.Repeat("s", 399) + "\n" // 400 bytes: in the 385-512 margin window
		src := "cat <<'A' <<B\n" + bodyA + "A\n" + bodyB + "B\n"

		got := ScanContent(src)

		if len(got) != 2 {
			t.Fatalf("expected 2 heredocs, got %d: %+v", len(got), got)
		}
		if got[0].Unquoted {
			t.Fatalf("opener A (<<'A'>) is quoted, must not be marked Unquoted, got %+v", got[0])
		}
		if !got[1].Unquoted {
			t.Fatalf("opener B (bare <<B, dequeued after quoted <<'A'>) must be marked Unquoted on its own merits, got %+v", got[1])
		}
		if got[1].Size != 400 {
			t.Fatalf("expected opener B's body size 400, got %d", got[1].Size)
		}
		// 400 > unquotedThreshold(defaultBudget) (384) always holds given the
		// size assertion above, so this next check adds no independent
		// coverage. It is kept as documentation of the margin window itself
		// -- calling the real production unquotedThreshold rather than
		// re-implementing the 384 constant -- not to catch a regression the
		// size check wouldn't already catch.
		if got[1].Size <= unquotedThreshold(defaultBudget) {
			t.Fatalf("expected opener B's body over the unquoted margin (%d), got %d", unquotedThreshold(defaultBudget), got[1].Size)
		}
	})

	t.Run("bare_then_dash_quoted_second_opener_stays_quoted", func(t *testing.T) {
		bodyA := strings.Repeat("a", 50) + "\n"
		bodyB := strings.Repeat("b", 50) + "\n"
		// <<-'B' both dash-strips leading tabs on close AND is quoted; the
		// closing line's leading tab exercises the dash form too.
		src := "cat <<A <<-'B'\n" + bodyA + "A\n" + bodyB + "\tB\n"

		got := ScanContent(src)

		if len(got) != 2 {
			t.Fatalf("expected 2 heredocs, got %d: %+v", len(got), got)
		}
		if !got[0].Unquoted {
			t.Fatalf("opener A (bare <<A) must be marked Unquoted, got %+v", got[0])
		}
		if got[1].Unquoted {
			t.Fatalf("opener B (<<-'B'>, dequeued after bare <<A) must stay quoted, not inherit A's bare-ness, got %+v", got[1])
		}
	})

	t.Run("three_openers_alternating_quoted_survive_two_dequeues", func(t *testing.T) {
		bodyA := strings.Repeat("a", 30) + "\n"
		bodyB := strings.Repeat("b", 30) + "\n"
		bodyC := strings.Repeat("c", 30) + "\n"
		src := "cat <<'A' <<B <<'C'\n" + bodyA + "A\n" + bodyB + "B\n" + bodyC + "C\n"

		got := ScanContent(src)

		if len(got) != 3 {
			t.Fatalf("expected 3 heredocs, got %d: %+v", len(got), got)
		}
		wantUnquoted := []bool{false, true, false}
		for i, h := range got {
			if h.Line != 1 {
				t.Fatalf("heredoc %d: expected opener line 1 (all three openers share the command line), got %d", i, h.Line)
			}
			if h.Unquoted != wantUnquoted[i] {
				t.Fatalf("heredoc %d: expected Unquoted=%v, got %v (%+v)", i, wantUnquoted[i], h.Unquoted, h)
			}
		}
	})
}

// --- tree walking tests --------------------------------------------------

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanTree_WalksShFilesOnlyAndComputesRepoRelativePaths(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	overBody := strings.Repeat("z", 600) + "\n"

	mustWriteFile(t, filepath.Join(scriptsDir, "offender.sh"), "cat <<EOF\n"+overBody+"EOF\n")
	mustWriteFile(t, filepath.Join(scriptsDir, "lib", "nested.sh"), "cat <<EOF\n"+overBody+"EOF\n")
	mustWriteFile(t, filepath.Join(scriptsDir, "notes.txt"), "cat <<EOF\n"+overBody+"EOF\n")
	mustWriteFile(t, filepath.Join(scriptsDir, "clean.sh"), "cat <<EOF\nshort\nEOF\n")

	violations, err := ScanTree(scriptsDir, defaultBudget)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if _, ok := violations["scripts/offender.sh"]; !ok {
		t.Fatalf("expected scripts/offender.sh in violations, got %v", violations)
	}
	if _, ok := violations["scripts/lib/nested.sh"]; !ok {
		t.Fatalf("expected scripts/lib/nested.sh in violations, got %v", violations)
	}
	if _, ok := violations["scripts/notes.txt"]; ok {
		t.Fatalf(".txt file must not be scanned, got %v", violations)
	}
	if _, ok := violations["scripts/clean.sh"]; ok {
		t.Fatalf("clean.sh has no over-budget heredoc, must not appear in violations")
	}
}

// TestScanTree_UnquotedHeredocFlaggedUnderRawBudgetButOverMargin guards
// #5085: an UNQUOTED heredoc must be flagged once its literal source crosses
// the stricter runtime-expansion margin (384 bytes for the default 512-byte
// budget), even though its 450-byte literal source is under the raw budget,
// because ${var}/$(cmd) expansion at runtime can push it past the real
// pipe-buffer deadlock threshold. A QUOTED heredoc of the identical size
// never expands and must stay clean.
func TestScanTree_UnquotedHeredocFlaggedUnderRawBudgetButOverMargin(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	body := strings.Repeat("a", 449) + "\n" // 450 bytes: 384 < 450 <= 512

	mustWriteFile(t, filepath.Join(scriptsDir, "unquoted-risk.sh"), "cat <<EOF\n"+body+"EOF\n")
	mustWriteFile(t, filepath.Join(scriptsDir, "quoted-safe.sh"), "cat <<'EOF'\n"+body+"EOF\n")

	violations, err := ScanTree(scriptsDir, defaultBudget)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if _, ok := violations["scripts/unquoted-risk.sh"]; !ok {
		t.Fatalf("expected unquoted 450-byte heredoc to be flagged (over the unquoted margin), got %v", violations)
	}
	if _, ok := violations["scripts/quoted-safe.sh"]; ok {
		t.Fatalf("quoted 450-byte heredoc never expands at runtime and must not be flagged, got %v", violations)
	}
}
