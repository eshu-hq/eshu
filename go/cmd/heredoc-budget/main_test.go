// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
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

// --- baseline parse/render tests -----------------------------------------

func TestBaselineRoundTrip(t *testing.T) {
	counts := map[string]int{
		"scripts/b.sh":    2,
		"scripts/a.sh":    1,
		"scripts/zero.sh": 0, // must be omitted from rendered output
	}

	rendered := RenderBaseline(counts)

	parsed, err := ParseBaseline(strings.NewReader(rendered))
	if err != nil {
		t.Fatalf("parse rendered baseline: %v", err)
	}
	if parsed["scripts/a.sh"] != 1 || parsed["scripts/b.sh"] != 2 {
		t.Fatalf("unexpected parsed counts: %v", parsed)
	}
	if _, ok := parsed["scripts/zero.sh"]; ok {
		t.Fatalf("zero-count entries must be omitted from the rendered baseline")
	}

	idxA := strings.Index(rendered, "scripts/a.sh")
	idxB := strings.Index(rendered, "scripts/b.sh")
	if idxA == -1 || idxB == -1 || idxA > idxB {
		t.Fatalf("expected deterministic sorted output, got:\n%s", rendered)
	}
}

func TestParseBaseline_IgnoresCommentsAndBlankLines(t *testing.T) {
	input := "# header comment\n\n  \nscripts/a.sh 3\n# trailing comment\nscripts/b.sh 1\n"

	parsed, err := ParseBaseline(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if parsed["scripts/a.sh"] != 3 || parsed["scripts/b.sh"] != 1 {
		t.Fatalf("unexpected parsed counts: %v", parsed)
	}
}

// --- baseline comparison (burn-down) tests -------------------------------

func TestCheckBaseline_NewFileWithViolationFails(t *testing.T) {
	current := map[string][]Violation{
		"scripts/new.sh": {{Path: "scripts/new.sh", Line: 3, Size: 600}},
	}
	baseline := map[string]int{}

	result := CheckBaseline(current, baseline)

	if result.OK {
		t.Fatalf("expected failure for a new offending file not in the baseline")
	}
	if _, ok := result.Failures["scripts/new.sh"]; !ok {
		t.Fatalf("expected scripts/new.sh in failures, got %v", result.Failures)
	}
}

func TestCheckBaseline_IncreasedCountFails(t *testing.T) {
	current := map[string][]Violation{
		"scripts/existing.sh": {
			{Path: "scripts/existing.sh", Line: 3, Size: 600},
			{Path: "scripts/existing.sh", Line: 20, Size: 700},
		},
	}
	baseline := map[string]int{"scripts/existing.sh": 1}

	result := CheckBaseline(current, baseline)

	if result.OK {
		t.Fatalf("expected failure when a baselined file's count increases")
	}
	if _, ok := result.Failures["scripts/existing.sh"]; !ok {
		t.Fatalf("expected scripts/existing.sh in failures, got %v", result.Failures)
	}
}

func TestCheckBaseline_DecreasedOrEqualCountPasses(t *testing.T) {
	current := map[string][]Violation{
		"scripts/existing.sh": {{Path: "scripts/existing.sh", Line: 3, Size: 600}},
	}

	decreased := CheckBaseline(current, map[string]int{"scripts/existing.sh": 2})
	if !decreased.OK {
		t.Fatalf("expected pass when count decreased (burn-down), got failures: %v", decreased.Failures)
	}

	equal := CheckBaseline(current, map[string]int{"scripts/existing.sh": 1})
	if !equal.OK {
		t.Fatalf("expected pass when count stayed equal, got failures: %v", equal.Failures)
	}
}

func TestCheckBaseline_UnknownCleanFilePasses(t *testing.T) {
	current := map[string][]Violation{
		"scripts/clean.sh": {}, // no violations
	}
	baseline := map[string]int{}

	result := CheckBaseline(current, baseline)

	if !result.OK {
		t.Fatalf("expected pass for a clean file absent from the baseline, got failures: %v", result.Failures)
	}
}

// --- CLI integration tests ------------------------------------------------

func TestRun_UpdateThenCheckPasses(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	overBody := strings.Repeat("z", 600) + "\n"
	mustWriteFile(t, filepath.Join(scriptsDir, "offender.sh"), "cat <<EOF\n"+overBody+"EOF\n")
	baselinePath := filepath.Join(scriptsDir, "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baselinePath, "-update"}, &stdout, &stderr); code != 0 {
		t.Fatalf("update run failed: code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-baseline", baselinePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("expected PASS on freshly generated baseline, got code=%d stderr=%s", code, stderr.String())
	}
}

func TestRun_NewOffenderFailsAfterBaseline(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baselinePath := filepath.Join(scriptsDir, "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baselinePath, "-update"}, &stdout, &stderr); code != 0 {
		t.Fatalf("update run failed: %s", stderr.String())
	}

	overBody := strings.Repeat("z", 600) + "\n"
	mustWriteFile(t, filepath.Join(scriptsDir, "new-offender.sh"), "cat <<EOF\n"+overBody+"EOF\n")

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"-baseline", baselinePath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for a new offending file, got PASS: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "new-offender.sh") {
		t.Fatalf("expected stderr to name the offending file, got: %s", stderr.String())
	}
}

func TestRun_RequiresBaselineFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit when -baseline is missing")
	}
}
