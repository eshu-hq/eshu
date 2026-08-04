// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// writeTrivyPair writes both artifacts the parity check compares: the local
// wrapper's skip_dirs assignment and the CI job's skip-dirs input. Passing an
// empty string for either skips writing that file, which is how the
// asymmetric-absence cases are built.
func writeTrivyPair(t *testing.T, root, localDirs, ciDirs string) {
	t.Helper()
	if localDirs != "" {
		scriptDir := filepath.Join(root, "scripts", "dev")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "#!/usr/bin/env bash\nset -euo pipefail\nskip_dirs=\"" + localDirs + "\"\nexec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
		if err := os.WriteFile(filepath.Join(scriptDir, "trivy-fs-local.sh"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ciDirs != "" {
		body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - uses: aquasecurity/trivy-action@master\n        with:\n          skip-dirs: " + ciDirs + "\n"
		writeWorkflow(t, root, "security-scan.yml", body)
	}
}

// driftFor returns the first DriftCheck error mentioning trivy skip-dirs, or ""
// when none does. Other checks share the same error slice, so matching on the
// subject keeps these tests from passing on an unrelated failure.
func driftFor(root string) string {
	reg := minimalReg(nil, nil, nil)
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "trivy") {
			return e.Error()
		}
	}
	return ""
}

// TestTrivySkipDirsParity_RealRepoMatches is the regression guard this change
// exists for. scripts/dev/trivy-fs-local.sh promises in its own comment that it
// uses "the same skip-dirs" as CI, but the two lists are unrelated string
// literals with no shared source, and they had drifted: the local list omitted
// go/cmd/mock-oidc-idp, so every local run exited 1 on the mock IdP's committed
// synthetic RSA key that CI deliberately suppresses.
//
// This asserts the committed tree, not a fixture, because a fixture cannot
// catch the real files drifting apart again.
func TestTrivySkipDirsParity_RealRepoMatches(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	if got := driftFor(repoRoot); got != "" {
		t.Fatalf("committed trivy-fs skip-dirs are out of parity: %s", got)
	}
}

// TestTrivySkipDirsParity_CIExtraDirDetected covers the direction that actually
// occurred: CI skips a directory the local wrapper does not, so the local gate
// reports noise CI suppresses.
func TestTrivySkipDirsParity_CIExtraDirDetected(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures,examples", "tests/fixtures,examples,go/cmd/mock-oidc-idp")

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected drift when CI skips a directory the local script does not")
	}
	if !containsAll(got, "go/cmd/mock-oidc-idp", "local-only noise") {
		t.Errorf("drift should name the missing directory and the direction, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalExtraDirDetected covers the more dangerous
// direction: the local wrapper skips something CI scans, so the local gate goes
// green on a finding CI will flag.
func TestTrivySkipDirsParity_LocalExtraDirDetected(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures,go/internal/secretstuff", "tests/fixtures")

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected drift when the local script skips a directory CI scans")
	}
	if !containsAll(got, "go/internal/secretstuff", "local blind spot") {
		t.Errorf("drift should name the extra directory and the blind-spot direction, got: %s", got)
	}
}

// TestTrivySkipDirsParity_OrderIrrelevant proves the check compares sets
// rather than strings. `--skip-dirs` is an unordered, comma-split pflag list
// that trivy tolerates duplicate and trailing-empty entries in (verified
// against trivy 0.72.0: `--skip-dirs "a,a,"` behaves identically to
// `--skip-dirs "a"`), so a reorder, a duplicate, or a trailing comma must not
// report drift; failing on any of those would be a false alarm that trains
// people to edit the check instead of the drift. Whitespace is NOT covered
// here -- see TestTrivySkipDirsParity_PaddingIsDrift for why padding must
// drift rather than be tolerated like these.
func TestTrivySkipDirsParity_OrderIrrelevant(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "examples,tests/fixtures,node_modules,node_modules,", "node_modules,examples,tests/fixtures")

	if got := driftFor(root); got != "" {
		t.Errorf("reordered, duplicated, and trailing-comma but identical sets must not drift, got: %s", got)
	}
}

// TestTrivySkipDirsParity_PaddingIsDrift pins that whitespace is compared
// literally, not trimmed. trivy's --skip-dirs is a pflag string slice: it
// comma-splits its argument but does NOT trim the resulting entries, so
// "secretdir " and "secretdir" are different directory patterns to trivy
// itself (verified against trivy 0.72.0: `--skip-dirs "secretdir "` does NOT
// skip `secretdir`). Trimming here would make a padded local list compare
// equal to an unpadded CI list while trivy actually skips different things
// locally -- a false green in the one check whose entire job is catching
// exactly that divergence.
func TestTrivySkipDirsParity_PaddingIsDrift(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures, examples ,node_modules", "tests/fixtures,examples,node_modules")

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected drift when only the local list is whitespace-padded")
	}
	if !containsAll(got, `" examples "`, "local blind spot") {
		t.Errorf("drift should name the whitespace-differing entry so a reader can act on it, got: %s", got)
	}
}

// TestTrivySkipDirsParity_MissingDeclarationFailsLoudly pins that the check
// reports rather than skips when it cannot find its subject. A parity check
// that silently passes when the marker moved is worse than no check, because
// the green result reads as proof the lists agree.
func TestTrivySkipDirsParity_MissingDeclarationFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures", "tests/fixtures")
	// Rewrite the script with the assignment renamed, as a refactor would.
	scriptPath := filepath.Join(root, "scripts", "dev", "trivy-fs-local.sh")
	renamed := "#!/usr/bin/env bash\nskipdirs=\"tests/fixtures\"\nexec trivy fs .\n"
	if err := os.WriteFile(scriptPath, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the skip_dirs declaration cannot be found")
	}
	if !containsAll(got, "found 0", "want exactly 1") {
		t.Errorf("error should say the declaration count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_DuplicateDeclarationFailsLoudly is the other arity
// failure: a second assignment means the check can no longer tell which one
// governs, so it must refuse rather than pick the first.
func TestTrivySkipDirsParity_DuplicateDeclarationFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures", "tests/fixtures")
	scriptPath := filepath.Join(root, "scripts", "dev", "trivy-fs-local.sh")
	doubled := "#!/usr/bin/env bash\nskip_dirs=\"tests/fixtures\"\nskip_dirs=\"examples\"\nexec trivy fs .\n"
	if err := os.WriteFile(scriptPath, []byte(doubled), 0o644); err != nil {
		t.Fatal(err)
	}

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when two skip_dirs declarations exist")
	}
	if !containsAll(got, "found 2", "want exactly 1") {
		t.Errorf("error should say the declaration count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_AsymmetricAbsenceDetected pins the one-sided case.
// Absent-on-both is legitimately skipped (this package's drift fixtures build
// minimal repos with neither artifact), but exactly one present means the local
// wrapper and its CI job came apart, which is the drift itself.
func TestTrivySkipDirsParity_AsymmetricAbsenceDetected(t *testing.T) {
	t.Parallel()

	t.Run("ci_only", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
		writeTrivyPair(t, root, "", "tests/fixtures")
		if got := driftFor(root); !containsAll(got, "security-scan.yml", "trivy-fs-local.sh") {
			t.Errorf("expected drift naming both artifacts, got: %s", got)
		}
	})

	t.Run("local_only", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
		writeTrivyPair(t, root, "tests/fixtures", "")
		if got := driftFor(root); !containsAll(got, "trivy-fs-local.sh", "security-scan.yml") {
			t.Errorf("expected drift naming both artifacts, got: %s", got)
		}
	})
}

// TestTrivySkipDirsParity_AbsentOnBothSkipped guards the escape hatch that lets
// this package's other drift fixtures keep passing: a repo with neither
// artifact has no parity to check, and must not fail on the absence.
func TestTrivySkipDirsParity_AbsentOnBothSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	if got := driftFor(root); got != "" {
		t.Errorf("a repo with neither artifact must not report trivy drift, got: %s", got)
	}
}

// TestTrivySkipDirsParity_AnchoredAgainstCommentedDeclaration pins that both
// regexes are anchored to the start of the line (mod leading whitespace on
// the YAML side), not merely containing the marker text anywhere on the line.
// Dropping ^ from trivyLocalSkipDirsRE, or loosening trivyCISkipDirsRE's
// leading whitespace class to `.*`, would let a commented-out declaration
// count as a second, spurious match; this test gives both mutations
// something to break.
func TestTrivySkipDirsParity_AnchoredAgainstCommentedDeclaration(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)

	scriptDir := filepath.Join(root, "scripts", "dev")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptBody := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"# skip_dirs=\"old,list\"\n" +
		"skip_dirs=\"tests/fixtures\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "trivy-fs-local.sh"), []byte(scriptBody), 0o644); err != nil {
		t.Fatal(err)
	}

	workflowBody := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          # skip-dirs: a,b\n" +
		"          skip-dirs: tests/fixtures\n"
	writeWorkflow(t, root, "security-scan.yml", workflowBody)

	if got := driftFor(root); got != "" {
		t.Errorf("a commented-out declaration must not create a second match or false drift, got: %s", got)
	}
}

// TestTrivySkipDirsParity_QuotedCIValueParsesCleanly pins that a quoted YAML
// scalar value ("a,b" or 'a,b') is unwrapped before the comma-split, not
// carried into the compared set. Without stripping, the quote characters land
// inside the first and last captured entries, so a routine quoting-style
// change in security-scan.yml would report a fabricated bidirectional drift
// naming a directory that does not exist.
func TestTrivySkipDirsParity_QuotedCIValueParsesCleanly(t *testing.T) {
	t.Parallel()

	t.Run("double_quoted", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
		writeTrivyPair(t, root, "tests/fixtures,examples", `"tests/fixtures,examples"`)
		if got := driftFor(root); got != "" {
			t.Errorf("a double-quoted CI value with the same set must not drift, got: %s", got)
		}
	})

	t.Run("single_quoted", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
		writeTrivyPair(t, root, "tests/fixtures,examples", `'tests/fixtures,examples'`)
		if got := driftFor(root); got != "" {
			t.Errorf("a single-quoted CI value with the same set must not drift, got: %s", got)
		}
	})
}

// TestTrivySkipDirsParity_CIRegexDoesNotCaptureAcrossLines pins that
// trivyCISkipDirsRE cannot bridge a newline between the "skip-dirs:" key and
// its value. \s matches \n, so a bare "skip-dirs:" key immediately followed by
// an unrelated line would let a \s*-based gap swallow the line break and
// capture that unrelated line's first token as if it were the value --
// silently misreading the input instead of refusing to guess.
func TestTrivySkipDirsParity_CIRegexDoesNotCaptureAcrossLines(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyPair(t, root, "tests/fixtures", "")
	workflowBody := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs:\n          ignore-unfixed\n"
	writeWorkflow(t, root, "security-scan.yml", workflowBody)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an arity error when skip-dirs has no same-line value")
	}
	if !containsAll(got, "found 0") {
		t.Errorf("a value-less skip-dirs key must not capture the next line's token as its value, got: %s", got)
	}
}
