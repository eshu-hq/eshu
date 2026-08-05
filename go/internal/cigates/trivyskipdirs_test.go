// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// This file, trivyskipdirs_helper_test.go, and trivyskipdirs_ci_test.go cover
// checkTrivySkipDirsParity's four assertions after #5925's redesign and
// #5927's tightening: the specs file is well-formed, scripts/lib/trivy-skip-dirs.sh
// (the shared derivation helper) references it exactly once,
// scripts/dev/trivy-fs-local.sh both `source`s the HELPER exactly once and
// calls the function it defines (trivy_skip_dirs_csv) exactly once -- not
// merely mentions the helper's path -- and the CI workflow's trivy-fs step is
// wired to a step held to that same source-and-call proof. See
// trivyskipdirs.go's package doc and AGENTS.md for the narrative, including
// the wiring-not-value-flow boundary this check deliberately stops at.

// driftFor returns the first DriftCheck error mentioning trivy skip-dirs, or
// "" when none does. Other checks share the same error slice, so matching on
// the subject keeps these tests from passing on an unrelated failure.
func driftFor(root string) string {
	reg := minimalReg(nil, nil, nil)
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "trivy") {
			return e.Error()
		}
	}
	return ""
}

// validSpecsBody is a well-formed specs/trivy-skip-dirs.txt: a comment line
// (which must not count as an entry) plus two directory entries.
const validSpecsBody = "# rationale\ntests/fixtures\nexamples\n"

// validHelperScriptBody is a well-formed scripts/lib/trivy-skip-dirs.sh: it
// references the specs path exactly once, and -- #5925 (single-source
// review) F6 -- uses the same filters the real committed helper does: a
// trailing-CR-only `sed` strip (a bare `tr -d '\r'` would also delete an
// embedded CR the Go side keeps) and an indentation-tolerant `#`-comment
// filter (a column-0-anchored `grep -v '^#'` would emit an indented comment
// as an entry, disagreeing with the Go side's HasPrefix(TrimSpace(line), "#")).
func validHelperScriptBody() string {
	return "#!/usr/bin/env bash\n" +
		"# shared trivy skip-dirs derivation (see specs/trivy-skip-dirs.txt)\n" +
		"trivy_skip_dirs_csv() {\n" +
		"\tlocal repo_root=\"$1\"\n" +
		"\tsed $'s/\\r$//' <\"${repo_root}/specs/trivy-skip-dirs.txt\" | grep -v '^[[:space:]]*#' | " +
		"grep -v '^[[:space:]]*$' | paste -sd, -\n" +
		"}\n"
}

// validLocalScriptBody sources the HELPER exactly once and calls
// trivy_skip_dirs_csv exactly once, the same way the real
// scripts/dev/trivy-fs-local.sh does.
func validLocalScriptBody() string {
	return "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"# mirrors scripts/lib/trivy-skip-dirs.sh\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
}

// validWorkflowBody is a trivy-fs job whose trivy-action step's skip-dirs
// input is exactly a steps-output expression, and whose producing step
// ("skipdirs") invokes the helper in its run: block -- the shape
// checkCIWorkflowSkipDirsFromHelper requires.
func validWorkflowBody() string {
	return "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        shell: bash\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
}

// writeTrivyArtifacts writes all four artifacts checkTrivySkipDirsParity
// wires together, using the "valid" bodies above except where overridden by a
// non-empty override argument. An empty body skips writing that artifact.
func writeTrivyArtifacts(t *testing.T, root, specsBody, helperBody, scriptBody, workflowBody string) {
	t.Helper()
	if specsBody != "" {
		if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "specs", "trivy-skip-dirs.txt"), []byte(specsBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if helperBody != "" {
		libDir := filepath.Join(root, "scripts", "lib")
		if err := os.MkdirAll(libDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(libDir, "trivy-skip-dirs.sh"), []byte(helperBody), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if scriptBody != "" {
		scriptDir := filepath.Join(root, "scripts", "dev")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scriptDir, "trivy-fs-local.sh"), []byte(scriptBody), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if workflowBody != "" {
		writeWorkflow(t, root, "security-scan.yml", workflowBody)
	}
}

// TestTrivySkipDirsParity_RealRepoMatches is the regression guard this check
// exists for: the committed specs file, helper, script, and workflow must
// actually be wired together, not merely present. This asserts the committed
// tree, not a fixture, because a fixture cannot catch the real files drifting
// apart again.
func TestTrivySkipDirsParity_RealRepoMatches(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	if got := driftFor(repoRoot); got != "" {
		t.Fatalf("committed trivy-fs skip-dirs wiring is broken: %s", got)
	}
}

// TestTrivySkipDirsParity_ValidQuadrupleClean pins the synthetic happy path:
// four artifacts that satisfy every assertion must not drift. This is the
// fixture every other test in this file, trivyskipdirs_helper_test.go, and
// trivyskipdirs_ci_test.go perturbs one field of at a time.
func TestTrivySkipDirsParity_ValidQuadrupleClean(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	if got := driftFor(root); got != "" {
		t.Errorf("a valid specs/helper/script/workflow quadruple must not drift, got: %s", got)
	}
}

// TestTrivySkipDirsParity_AllArtifactsAbsentSkipped guards the escape hatch
// that lets this package's other drift fixtures keep passing: a repo with
// none of the four artifacts has no parity to check, and must not fail on
// their absence.
func TestTrivySkipDirsParity_AllArtifactsAbsentSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	if got := driftFor(root); got != "" {
		t.Errorf("a repo with none of the four artifacts must not report trivy drift, got: %s", got)
	}
}

// ── specs file assertions ───────────────────────────────────────────────────

// TestTrivySkipDirsParity_SpecsFileMissingFailsLoudly pins that a missing
// specs file is reported even when the other artifacts exist and would
// otherwise be internally consistent.
func TestTrivySkipDirsParity_SpecsFileMissingFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "", validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when specs/trivy-skip-dirs.txt is missing")
	}
	if !containsAll(got, "cannot read", "trivy-skip-dirs.txt") {
		t.Errorf("error should say the specs file could not be read, got: %s", got)
	}
}

// TestTrivySkipDirsParity_SpecsFileEmptyFailsLoudly pins that a specs file
// with no directory entries -- only comments and blank lines -- is drift, not
// a vacuous pass.
func TestTrivySkipDirsParity_SpecsFileEmptyFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "# only a comment\n\n", validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the specs file has no directory entries")
	}
	if !containsAll(got, "no directory entries") {
		t.Errorf("error should say the specs file has no entries, got: %s", got)
	}
}

// TestTrivySkipDirsParity_SpecsFileDuplicateEntryFailsLoudly pins that a
// repeated directory entry is reported by name, not silently deduplicated.
func TestTrivySkipDirsParity_SpecsFileDuplicateEntryFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "tests/fixtures\nexamples\ntests/fixtures\n",
		validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the specs file lists a directory twice")
	}
	if !containsAll(got, `"tests/fixtures"`, "more than once") {
		t.Errorf("error should name the duplicated entry, got: %s", got)
	}
}

// TestTrivySkipDirsParity_SpecsFileEntryWhitespaceFailsLoudly pins #5925 F5's
// Go-side half: an entry with leading or trailing whitespace is rejected
// rather than silently trimmed, because the shell derivation does not trim
// interior whitespace the way a silent Go trim would hide.
func TestTrivySkipDirsParity_SpecsFileEntryWhitespaceFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "tests/fixtures\n examples\n",
		validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when a specs entry has leading whitespace")
	}
	if !containsAll(got, "leading or trailing whitespace") {
		t.Errorf("error should say the entry has incidental whitespace, got: %s", got)
	}
}

// TestTrivySkipDirsParity_SpecsFileCRLFLineEndingsAccepted pins that a CRLF
// line ending is treated as a line-ending artifact, not entry content --
// otherwise every specs file saved by a Windows editor would fail this check
// even though its entries are otherwise clean (#5925 F5).
func TestTrivySkipDirsParity_SpecsFileCRLFLineEndingsAccepted(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "tests/fixtures\r\nexamples\r\n",
		validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	if got := driftFor(root); got != "" {
		t.Errorf("a CRLF-saved specs file with otherwise-clean entries must not drift, got: %s", got)
	}
}

// TestTrivySkipDirsParity_SpecsFileCatchAllEntryFailsLoudly pins #5925 F6: an
// entry that would disable the whole-tree secret scanner entirely -- ".",
// "*", or any absolute path -- is rejected with a clear message. It also
// pins #5925 (single-source review) F1: a comma inside an entry is rejected
// too, because ',' is the delimiter the shared derivation's `paste -sd, -`
// joins entries with -- an entry like "examples,." would otherwise smuggle a
// catch-all past every per-entry check (each individual entry is neither ".",
// "*", nor absolute) while the comma-joined value trivy actually receives
// contains a bare "." skip-dir.
//
// "/" (and any other entry that normalizes to the empty string, e.g. "//" or
// "/.") is a DIFFERENT case, pinned separately below: proven against real
// trivy 0.72.0 on a fixture with 2 planted secrets, --skip-dirs "" left both
// findable, unlike --skip-dirs '.' which found zero (#5927 round-7 review
// F1). It is rejected because it is dead weight, not because it disables the
// scan -- do not assert "catch-all" against its message.
//
// Round-2 review (P1-1) proved the original entry-specific literal check --
// reject only ".", "*", or a leading "/" as the WHOLE entry -- defeated
// against real trivy 0.72.0: "**", "**/*", "./", ".//", "./.", and "?" all
// slipped through and disabled the scan (0 secrets found on a fixture tree
// with 2 planted). "./", ".//", and "./." work because trivy's own
// CleanSkipPaths runs filepath.Clean then trims a leading "/", normalizing
// all three back to the bare "." the original check rejected; "**" and
// "**/*" match every path under doublestar. The fix closes the CLASS instead
// of adding a fourth, fifth, and sixth literal: any glob metacharacter is
// rejected outright, and every entry is normalized the way trivy itself does
// before the catch-all question is asked, so a differently-spelled path that
// still normalizes to "." is caught structurally. ".." is rejected too, but
// for a different, narrower reason stated in its own error message: proven
// against real trivy 0.72.0, "--skip-dirs '..'" does NOT zero out coverage
// the way "." does (both planted secrets stayed findable) -- it is rejected
// because it escapes the repository root the list is defined relative to,
// not because it disables the scan.
//
// Round-3 review (P2-1) closed the SAME kind of instance-vs-class gap on the
// ".." check specifically: an exact `norm == ".."` literal check rejected
// only the two-character entry, leaving "../.." and "../foo" -- both of
// which normalize to a value sharing the "../" prefix and escape the
// repository root for the identical reason -- unrejected. "../.." and
// "../foo" below pin that the check now rejects the whole "../"-prefixed
// class, not only the bare literal.
func TestTrivySkipDirsParity_SpecsFileCatchAllEntryFailsLoudly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		entry string
		want  string
	}{
		{".", "catch-all"},
		{"..", "escapes the repository root"},
		{"../..", "escapes the repository root"},
		{"../foo", "escapes the repository root"},
		{"./", "catch-all"},
		{".//", "catch-all"},
		{"./.", "catch-all"},
		{"/etc", "absolute path"},
		{"*", "glob metacharacter"},
		{"**", "glob metacharacter"},
		{"**/*", "glob metacharacter"},
		{"?", "glob metacharacter"},
		{"examples,.", "comma"},
		{"examples,*", "comma"},
		{"examples,/etc", "comma"},
		{"a,b", "comma"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.entry, func(t *testing.T) {
			t.Parallel()

			root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
			writeTrivyArtifacts(t, root, "tests/fixtures\n"+c.entry+"\n",
				validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

			got := driftFor(root)
			if got == "" {
				t.Fatalf("expected an error when the specs file lists entry %q", c.entry)
			}
			if !containsAll(got, c.want) {
				t.Errorf("error should mention %q, got: %s", c.want, got)
			}
		})
	}
}

// TestTrivySkipDirsParity_SpecsFileEmptyNormalizingEntryFailsLoudly pins
// #5925/#5927 round-7 review F1: an entry that normalizes to the empty
// string -- "/", "//", or "/." -- is rejected too, but NOT for the
// catch-all reason. Proven against real trivy 0.72.0 on a fixture with 2
// planted secrets: --skip-dirs '.' found 0 (genuinely disables the scan),
// while --skip-dirs "" (and '/', '//', '/.') found 2 -- both secrets stayed
// findable. Read against trivy's own source (pkg/fanal/utils/utils.go),
// CleanSkipPaths does not drop an empty-after-clean entry; SkipPath's
// doublestar.Match against an empty pattern simply never matches a real
// repo-relative path, so the entry disables nothing. It is rejected because
// it is dead weight, not because it is a catch-all -- the negative
// assertion below fails if a future edit reverts to the old, false
// "catch-all" wording for this case.
func TestTrivySkipDirsParity_SpecsFileEmptyNormalizingEntryFailsLoudly(t *testing.T) {
	t.Parallel()

	for _, entry := range []string{"/", "//", "/."} {
		entry := entry
		t.Run(entry, func(t *testing.T) {
			t.Parallel()

			root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
			writeTrivyArtifacts(t, root, "tests/fixtures\n"+entry+"\n",
				validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

			got := driftFor(root)
			if got == "" {
				t.Fatalf("expected an error when the specs file lists entry %q", entry)
			}
			if !containsAll(got, "dead weight") {
				t.Errorf("error should mention %q, got: %s", "dead weight", got)
			}
			if strings.Contains(got, "catch-all") {
				t.Errorf("error must NOT claim %q is a catch-all -- it does not disable the scan, got: %s", entry, got)
			}
		})
	}
}

// TestTrivySkipDirsParity_SpecsFileTrailingCommentEntryFailsLoudly pins
// single-source review P2-2: only a WHOLE-LINE comment is supported (a line
// whose first non-whitespace character is '#'), never a trailing comment
// appended after a real entry. Proven against the real committed helper and
// trivy 0.72.0: `trivy fs --skip-dirs 'alpha'` skips alpha (1 secret found on
// a 2-secret fixture), but `trivy fs --skip-dirs 'alpha # rationale'` skips
// nothing (2 secrets found) -- the shared shell derivation's
// `grep -v '^[[:space:]]*#'` only drops WHOLE comment lines, so "alpha #
// rationale" is not "alpha", it is a literal (bogus) directory. That failure
// mode is fail-closed (trivy-fs goes red), but it points a contributor at the
// fixtures rather than the malformed specs line -- exactly the misdirection
// this parity check exists to avoid. Rejecting any entry containing '#'
// during registry drift-check catches it before a scan ever runs.
func TestTrivySkipDirsParity_SpecsFileTrailingCommentEntryFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, "tests/fixtures\nexamples # x\n",
		validHelperScriptBody(), validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when a specs entry has a trailing comment")
	}
	if !containsAll(got, "trailing comment") {
		t.Errorf("error should say the entry has a trailing comment, got: %s", got)
	}
}
