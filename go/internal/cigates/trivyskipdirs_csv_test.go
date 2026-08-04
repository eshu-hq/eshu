// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This file closes #5925 F11: checkTrivySkipDirsParity only counts path
// REFERENCES (trivySkipDirsSpecEntries's parsed entries are discarded by its
// caller); nothing anywhere compares them to what the shell helper,
// trivy_skip_dirs_csv, actually prints. Weakening any of the helper's filters
// -- its `sed $'s/\r$//'` line-ending strip, its `grep -v '^[[:space:]]*#'`
// comment filter, or its `paste -sd,` delimiter -- passes every other check in
// this package. These tests shell out to bash to run the REAL committed helper
// and assert its stdout is byte-identical to
// strings.Join(trivySkipDirsSpecEntries(...), ",") -- proving the shell and
// Go parsers agree on OUTPUT, not merely that both reference the same path.
//
// package cigates (not cigates_test) so these tests can call the unexported
// trivySkipDirsSpecEntries directly, the same internal-test-package pattern
// required_workflow_test.go already uses in this directory.
//
// Shelling out to bash here does not revisit the "prove wiring, not value
// flow" boundary trivyskipdirs.go's package doc and AGENTS.md draw for
// checkTrivySkipDirsParity itself: that boundary is about what the
// PRODUCTION drift check (Select/Validate/DriftCheck, which must stay a
// pure, hermetic, network/Docker/credential-free function per this
// package's AGENTS.md) is allowed to interpret. A _test.go file asserting
// the shell helper's behavior is not part of that production surface, and
// os/exec already appears in this repo's Go test suites elsewhere (e.g.
// go/internal/collector's git tests) without exception. This package's
// AGENTS.md states no rule against os/exec in tests, only that Select must
// not touch git/filesystem/external services and that Validate must
// accumulate rather than short-circuit -- neither applies here.

// runTrivySkipDirsCSVHelper shells out to bash, sources the real committed
// scripts/lib/trivy-skip-dirs.sh from repoRoot, and returns
// trivy_skip_dirs_csv(specsRoot)'s stdout with the trailing newline trimmed.
// specsRoot is a separate parameter from repoRoot so the CRLF/whitespace
// fixture test below can point the REAL helper at a synthetic specs file
// while still sourcing the real, committed derivation code -- not a copy of
// it. t.Skip, not t.Fatal, when bash is unavailable: this test proves a
// property of the committed shell helper, and a machine without bash cannot
// prove or disprove that property, which is a different condition from the
// helper being broken.
func runTrivySkipDirsCSVHelper(t *testing.T, repoRoot, specsRoot string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available on this machine -- skipping shell/Go parity proof")
	}

	helperPath := filepath.Join(repoRoot, trivySkipDirsHelperPath)
	// #nosec G204 -- helperPath and specsRoot are test-constructed, not
	// attacker input.
	cmd := exec.Command("bash", "-c",
		`source "$1"; trivy_skip_dirs_csv "$2"`, "bash", helperPath, specsRoot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trivy_skip_dirs_csv(%q) failed: %v\noutput: %s", specsRoot, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// TestTrivySkipDirsCSV_RealRepoMatchesGoParsing is the regression guard F11
// asks for over the committed specs file: the shell helper's CSV output and
// trivySkipDirsSpecEntries's parsed, comma-joined entries must be
// byte-identical. This is the proof checkTrivySkipDirsParity itself does not
// provide -- it only proves both sides REFERENCE the same files, never that
// their derivations agree on VALUE.
func TestTrivySkipDirsCSV_RealRepoMatchesGoParsing(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	specsPath := filepath.Join(repoRoot, trivySkipDirsSpecPath)

	entries, err := trivySkipDirsSpecEntries(specsPath)
	if err != nil {
		t.Fatalf("trivySkipDirsSpecEntries(%q): %v", specsPath, err)
	}
	want := strings.Join(entries, ",")

	got := runTrivySkipDirsCSVHelper(t, repoRoot, repoRoot)
	if got != want {
		t.Fatalf("shell helper and Go parser disagree on the committed specs file:\n"+
			"  shell: %q\n  go:    %q", got, want)
	}
}

// TestTrivySkipDirsCSV_CRLFWhitespaceFixtureMatchesGoParsing proves the same
// agreement over a synthetic specs file with CRLF line endings and a
// whitespace-only line -- the exact shape #5925 F5 added Go-side handling
// for (trivySkipDirsSpecEntries's '\r' trim, TrimSpace-empty-line skip). The
// REAL committed helper is sourced; only the specs file it reads is a
// fixture, so this proves the shell side's CRLF/blank-line handling matches
// the Go side's, not merely that each was tested in isolation.
func TestTrivySkipDirsCSV_CRLFWhitespaceFixtureMatchesGoParsing(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	fixtureRoot := t.TempDir()
	specsDir := filepath.Join(fixtureRoot, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The indented comment is the shape that made the two parsers disagree
	// before the helper's filter was widened to '^[[:space:]]*#': the Go side
	// has always treated a '#' after leading whitespace as a comment (it
	// checks HasPrefix on the TrimSpace'd line), and specs/trivy-skip-dirs.txt
	// promises exactly that, but a column-0-anchored grep emitted the line as
	// an entry. Keeping it here pins the agreement rather than the Go half
	// alone.
	// The embedded CR in "tests/fix\rtures" pins the OTHER filter the helper
	// changed. A trailing-CR-only fixture cannot tell `sed $'s/\r$//'` from a
	// bare `tr -d '\r'` -- every CR sits immediately before a newline, where
	// the two are indistinguishable -- so reverting to `tr` would leave the
	// suite green while silently deleting a CR the Go side (strings.TrimSuffix,
	// trailing only) keeps. With this line, `tr` yields "tests/fixtures" where
	// Go yields "tests/fix\rtures", and the two parsers disagree loudly.
	fixtureBody := "# rationale\r\n  # indented rationale\r\ntests/fix\rtures\r\n   \r\nexamples\r\n"
	fixturePath := filepath.Join(specsDir, "trivy-skip-dirs.txt")
	if err := os.WriteFile(fixturePath, []byte(fixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := trivySkipDirsSpecEntries(fixturePath)
	if err != nil {
		t.Fatalf("trivySkipDirsSpecEntries(%q): %v", fixturePath, err)
	}
	want := strings.Join(entries, ",")

	got := runTrivySkipDirsCSVHelper(t, repoRoot, fixtureRoot)
	if got != want {
		t.Fatalf("shell helper and Go parser disagree on a CRLF/whitespace fixture:\n"+
			"  shell: %q\n  go:    %q", got, want)
	}
}
