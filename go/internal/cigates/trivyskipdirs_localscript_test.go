// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"
)

// This file covers the half of checkTrivySkipDirsParity that validates
// scripts/dev/trivy-fs-local.sh: it must both `source` the shared derivation
// helper, scripts/lib/trivy-skip-dirs.sh, exactly once and call the function
// it defines, trivy_skip_dirs_csv, exactly once -- not merely mention the
// helper's path (checkScriptInvokesHelper / checkSourcedAndCalled, #5927
// review). See trivyskipdirs_test.go for validLocalScriptBody, the
// happy-path fixture every test here perturbs, and writeTrivyArtifacts. This
// file was split out of trivyskipdirs_test.go to keep that file under the
// package's 500-line-per-file limit (see go/internal/cigates/AGENTS.md).
//
// #5927 review: checkFileReferencesPathOnce used to count MENTIONS of the
// helper's path in the local script, so a string literal or an echo
// mentioning the path -- while a hard-coded value actually governed the
// emitted skip-dirs -- read as "wired to the helper". The local script must
// now prove both halves of a real invocation: a `source` (or `.`) line that
// loads the helper's definitions, AND a call to the function it defines,
// trivy_skip_dirs_csv (checkScriptInvokesHelper / checkSourcedAndCalled).

// TestTrivySkipDirsParity_LocalScriptDoesNotReferenceHelperFailsLoudly pins
// that a script no longer invoking the helper at all -- e.g. reverted to its
// own derivation or a hard-coded list -- is drift.
func TestTrivySkipDirsParity_LocalScriptDoesNotReferenceHelperFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	hardcoded := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"skip_dirs=\"tests/fixtures,examples\"\nexec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), hardcoded, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script no longer references the helper")
	}
	if !containsAll(got, "does not `source`", "scripts/lib/trivy-skip-dirs.sh") {
		t.Errorf("error should say the script does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptMentionsHelperOnlyInEchoFailsLoudly pins
// the false-green the review found: a NON-comment mention of the helper's
// path -- here inside an echo string, not a `source` line -- must not read as
// "invokes the helper" while a hard-coded value governs the actual output.
func TestTrivySkipDirsParity_LocalScriptMentionsHelperOnlyInEchoFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"echo \"would use scripts/lib/trivy-skip-dirs.sh\"\n" +
		"skip_dirs=\"tests/fixtures,examples\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the only mention of the helper's path is inside an echo string")
	}
	if !containsAll(got, "does not `source`", "scripts/lib/trivy-skip-dirs.sh") {
		t.Errorf("error should say the script does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptSourcesHelperButNeverCallsFailsLoudly
// pins the other half: sourcing the helper alone does not run its
// derivation -- the script must also call trivy_skip_dirs_csv.
func TestTrivySkipDirsParity_LocalScriptSourcesHelperButNeverCallsFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"tests/fixtures,examples\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script sources the helper but never calls trivy_skip_dirs_csv")
	}
	if !containsAll(got, "never calls trivy_skip_dirs_csv", "not wired") {
		t.Errorf("error should say the script never calls trivy_skip_dirs_csv, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptCallsHelperButNeverSourcesFailsLoudly
// pins the symmetric gap: calling trivy_skip_dirs_csv without first sourcing
// the file that defines it cannot possibly be the real, governing call (bash
// would fail with "command not found" at runtime).
func TestTrivySkipDirsParity_LocalScriptCallsHelperButNeverSourcesFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script calls trivy_skip_dirs_csv without sourcing the helper")
	}
	if !containsAll(got, "does not `source`", "scripts/lib/trivy-skip-dirs.sh") {
		t.Errorf("error should say the script does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptSourcesHelperTwiceFailsLoudly pins the
// arity failure on the source half: two source lines mean the check cannot
// tell which invocation governs (e.g. a live one and one inside a dead
// branch).
func TestTrivySkipDirsParity_LocalScriptSourcesHelperTwiceFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script sources the helper twice")
	}
	if !containsAll(got, "sources", "2 time(s)", "want exactly 1") {
		t.Errorf("error should say the source count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptCallsHelperTwiceFailsLoudly pins #5925
// (single-source review) F4: the call-arity ">1" branch in
// checkSourcedAndCalled (trivySkipDirsCsvCallRE's default case) had no test
// anywhere in this package -- replacing that branch with `return nil` left
// all existing tests green, silently accepting a script that calls
// trivy_skip_dirs_csv more than once, which is exactly the
// live-call-plus-dead-branch-call ambiguity the arity rule exists to refuse
// (the same ambiguity TestTrivySkipDirsParity_LocalScriptSourcesHelperTwiceFailsLoudly
// pins for the source half).
func TestTrivySkipDirsParity_LocalScriptCallsHelperTwiceFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script calls trivy_skip_dirs_csv twice")
	}
	if !containsAll(got, "calls trivy_skip_dirs_csv", "2 time(s)", "want exactly 1") {
		t.Errorf("error should say the call count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptMissingPipefailFailsLoudly pins
// single-source review P2-3: the local script must `set -o pipefail` (the
// same failure-mode-parity requirement checkCIWorkflowSkipDirsFromHelper
// enforces CI-side via `shell: bash`, #5925 F12). A script that correctly
// sources and calls the helper but never sets pipefail would let an
// unreadable specs file fail silently inside trivy_skip_dirs_csv's
// `sed | grep | grep | paste` pipeline -- the pipeline's exit status is
// `paste`'s (0) -- so the script would derive an empty skip-dirs value
// instead of failing loudly, diverging from what CI's own comment on the
// producer step promises.
func TestTrivySkipDirsParity_LocalScriptMissingPipefailFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script does not set -o pipefail")
	}
	if !containsAll(got, "pipefail") {
		t.Errorf("error should say the script does not set pipefail, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptSetPlusOPipefailFailsLoudly pins
// #5925/#5927 round-7 review F2: `set +o pipefail` DISABLES pipefail (`+o`
// turns an option off in bash, `-o` turns it on), but trivyPipefailRE only
// checked for the word "set" followed eventually by the word "pipefail" --
// it never distinguished `+o` from `-o`, so a script that explicitly turns
// pipefail OFF read as having set it. Same fail-open class as the round-3
// (trailing-comment) and round-6 (command-separator) holes this file
// already closed; this one was neither closed nor documented.
func TestTrivySkipDirsParity_LocalScriptSetPlusOPipefailFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"set +o pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the local script explicitly disables pipefail with `set +o pipefail`")
	}
	if !containsAll(got, "pipefail") {
		t.Errorf("error should say the script does not set pipefail, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptPipefailOnlyInTrailingCommentFailsLoudly
// pins round-3 review P2-2: trivyPipefailRE must not let a trailing comment
// on a `set`-prefixed line satisfy the pattern. The comment below --
// `# -o pipefail` -- deliberately contains the exact `-o pipefail` shape the
// pattern requires, so this fixture also discriminates the '#'-in-the-
// negated-class element specifically (#5927 round-7 review P2-1): round-7
// added a requirement that "pipefail" follow a `-`-prefixed flag cluster
// containing `o`, and the ORIGINAL comment here (`# pipefail deliberately
// not set`) has no such cluster anywhere in it, so it stopped discriminating
// the '#' guard once that requirement landed -- the check would still fail
// this fixture with '#' removed from trivyPipefailRE's negated class,
// because there is still no "-o pipefail" run before the '#' either way.
// Putting a real "-o pipefail" INSIDE the comment restores the discriminating
// power: with '#' correctly in the negated class, the run stops before the
// comment and this does not establish pipefail (expected); with '#' dropped,
// the negated-class run would cross into the comment and find "-o pipefail"
// there, incorrectly matching.
func TestTrivySkipDirsParity_LocalScriptPipefailOnlyInTrailingCommentFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"set -x # -o pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the only mention of pipefail is inside a trailing comment")
	}
	if !containsAll(got, "pipefail") {
		t.Errorf("error should say the script does not set pipefail, got: %s", got)
	}
}

// TestTrivySkipDirsParity_LocalScriptPipefailMentionAfterSemicolonFailsLoudly
// pins round-6 review P3-1: trivyPipefailRE's right boundary excluded '#'
// (round-3 P2-2) but not a command separator, so a line STARTING with `set`
// that later mentions "pipefail" in an unrelated command sharing the same
// physical line -- after a ';', '&&', or '|' -- read as establishing it, the
// opposite failure direction (fail-OPEN) from every other limitation this
// package documents.
//
// The command after the ';' below -- `echo -o pipefail` -- deliberately
// contains a real "-o pipefail" run, not just the bare word "pipefail" the
// original fixture used (`printf "remember pipefail\n"`). #5927 round-7
// review P2-1 found that round-7's added requirement -- "pipefail" must
// follow a `-`-prefixed flag cluster containing `o` -- means a bare mention
// of the word no longer reaches a match even with ';' unguarded, so the
// original fixture stopped discriminating the ';&|' guard once that
// requirement landed: it would still correctly fail this check with ';&|'
// removed from trivyPipefailRE's negated class, because "remember pipefail"
// has no "-o"-shaped cluster anywhere in it. Using "echo -o pipefail"
// restores the discriminating power: with ';' correctly in the negated
// class, the run stops before the semicolon and this does not establish
// pipefail (expected); with ';&|' dropped, the negated-class run would cross
// the semicolon and find "-o pipefail" in the unrelated echo, incorrectly
// matching.
func TestTrivySkipDirsParity_LocalScriptPipefailMentionAfterSemicolonFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\n" +
		"set -e; echo -o pipefail\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when \"pipefail\" only appears in an unrelated command after a ';' on the same set-prefixed line")
	}
	if !containsAll(got, "pipefail") {
		t.Errorf("error should say the script does not set pipefail, got: %s", got)
	}
}

// TestTrivySkipDirsParity_CommentMentioningHelperPathDoesNotCountAsSecondReference
// pins that stripping WHOLE comment lines before counting is load-bearing:
// the real trivy-fs-local.sh has a comment line mentioning the helper path
// (for a human reading the script) in addition to the live reference, and
// that comment must not be counted as a second reference.
func TestTrivySkipDirsParity_CommentMentioningHelperPathDoesNotCountAsSecondReference(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"# see scripts/lib/trivy-skip-dirs.sh for the shared derivation\n" +
		"source scripts/lib/trivy-skip-dirs.sh\n" +
		"skip_dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"exec trivy fs --skip-dirs \"${skip_dirs}\" .\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), body, validWorkflowBody())

	if got := driftFor(root); got != "" {
		t.Errorf("a whole comment line mentioning the helper path must not count as a second reference, got: %s", got)
	}
}
