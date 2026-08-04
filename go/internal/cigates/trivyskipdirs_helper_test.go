// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"
)

// This file covers the half of checkTrivySkipDirsParity that is new in
// #5925 F2: scripts/lib/trivy-skip-dirs.sh -- the single shared skip-dirs
// derivation both scripts/dev/trivy-fs-local.sh and security-scan.yml invoke
// -- must itself reference specs/trivy-skip-dirs.txt exactly once. See
// trivyskipdirs_test.go for validHelperScriptBody, the happy-path fixture
// every test here perturbs, and writeTrivyArtifacts.

// TestTrivySkipDirsParity_HelperMissingFailsLoudly pins that a missing helper
// is reported even when the other artifacts exist and would otherwise be
// internally consistent -- the local script and CI workflow being correctly
// wired to a helper that does not exist is still drift.
func TestTrivySkipDirsParity_HelperMissingFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, validSpecsBody, "", validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when scripts/lib/trivy-skip-dirs.sh is missing")
	}
	if !containsAll(got, "cannot read", "trivy-skip-dirs.sh") {
		t.Errorf("error should say the helper could not be read, got: %s", got)
	}
}

// TestTrivySkipDirsParity_HelperDoesNotReferenceSpecFailsLoudly pins that a
// helper no longer reading the specs file at all -- e.g. reverted to a
// hard-coded list -- is drift.
func TestTrivySkipDirsParity_HelperDoesNotReferenceSpecFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	hardcoded := "#!/usr/bin/env bash\n" +
		"trivy_skip_dirs_csv() {\n\techo \"tests/fixtures,examples\"\n}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, hardcoded, validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the helper no longer references the specs file")
	}
	if !containsAll(got, "references", "0 time(s)", "want exactly 1") {
		t.Errorf("error should say the reference count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_HelperReferencesSpecTwiceFailsLoudly pins the other
// arity failure: two references mean the check cannot tell which one is the
// real, governing read (e.g. a live one and one inside a dead branch).
func TestTrivySkipDirsParity_HelperReferencesSpecTwiceFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"trivy_skip_dirs_csv() {\n" +
		"\tlocal repo_root=\"$1\"\n" +
		"\tsed $'s/\\r$//' <\"${repo_root}/specs/trivy-skip-dirs.txt\" | grep -v '^[[:space:]]*#' | " +
		"grep -v '^[[:space:]]*$' | paste -sd, -\n" +
		"}\n" +
		"legacy_dirs=\"$(cat specs/trivy-skip-dirs.txt)\"\n"
	writeTrivyArtifacts(t, root, validSpecsBody, body, validLocalScriptBody(), validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the helper references the specs file twice")
	}
	if !containsAll(got, "references", "2 time(s)", "want exactly 1") {
		t.Errorf("error should say the reference count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_CommentMentioningSpecPathInHelperDoesNotCountAsSecondReference
// pins that stripping WHOLE comment lines before counting is load-bearing for
// the helper too: a comment line mentioning the specs path (for a human
// reading the script) in addition to the live reference must not be counted
// as a second reference.
func TestTrivySkipDirsParity_CommentMentioningSpecPathInHelperDoesNotCountAsSecondReference(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"# see specs/trivy-skip-dirs.txt for the authoritative list\n" +
		"trivy_skip_dirs_csv() {\n" +
		"\tlocal repo_root=\"$1\"\n" +
		"\tsed $'s/\\r$//' <\"${repo_root}/specs/trivy-skip-dirs.txt\" | grep -v '^[[:space:]]*#' | " +
		"grep -v '^[[:space:]]*$' | paste -sd, -\n" +
		"}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, body, validLocalScriptBody(), validWorkflowBody())

	if got := driftFor(root); got != "" {
		t.Errorf("a whole comment line mentioning the specs path must not count as a second reference, got: %s", got)
	}
}
