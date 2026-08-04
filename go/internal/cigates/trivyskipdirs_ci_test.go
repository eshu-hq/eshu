// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"
)

// This file covers checkCIWorkflowSkipDirsFromHelper: the trivy-fs job's
// trivy-action step's skip-dirs input must be exactly a
// `${{ steps.<id>.outputs.<name> }}` expression, and the step with that id
// must both `source` scripts/lib/trivy-skip-dirs.sh -- the shared derivation
// helper -- and call the function it defines, trivy_skip_dirs_csv, in its
// run: block, over whole-line-comment-stripped content (checkSourcedAndCalled,
// #5927 review) -- not merely mention the helper's path. See
// trivyskipdirs_test.go for validWorkflowBody, the happy-path fixture every
// test here perturbs one field of.

// TestTrivySkipDirsParity_CIWorkflowMissingFailsLoudly pins that a missing
// workflow file is reported.
func TestTrivySkipDirsParity_CIWorkflowMissingFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), "")

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when security-scan.yml is missing")
	}
	if !containsAll(got, "cannot read", "security-scan.yml") {
		t.Errorf("error should say the workflow could not be read, got: %s", got)
	}
}

// TestTrivySkipDirsParity_CIWorkflowUnparseableFailsLoudly pins that a
// workflow that exists but cannot be parsed is drift-worthy on its own, not a
// skip -- a truly broken CI file must not read as parity.
func TestTrivySkipDirsParity_CIWorkflowUnparseableFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	invalid := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: \"unterminated\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), invalid)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the CI workflow YAML cannot be parsed")
	}
	if !containsAll(got, "cannot parse") {
		t.Errorf("error should say the workflow could not be parsed, got: %s", got)
	}
}

// TestTrivySkipDirsParity_MissingTrivyFSJobFailsLoudly pins that a workflow
// with no job keyed "trivy-fs" (e.g. a rename) errors rather than silently
// skipping.
func TestTrivySkipDirsParity_MissingTrivyFSJobFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	renamed := "name: Security Scan\non: [push]\njobs:\n  trivy-filesystem-scan:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n          skip-dirs: tests/fixtures\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), renamed)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when security-scan.yml has no trivy-fs job")
	}
	if !containsAll(got, "has no", "cannot locate the trivy filesystem scan") {
		t.Errorf("error should name the missing job, got: %s", got)
	}
}

// TestTrivySkipDirsParity_TwoTrivyActionStepsInJobFailsLoudly pins that two
// aquasecurity/trivy-action steps in the same trivy-fs job is ambiguous, not
// silently resolved to the first.
func TestTrivySkipDirsParity_TwoTrivyActionStepsInJobFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: node_modules\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the trivy-fs job has two trivy-action steps")
	}
	if !containsAll(got, "found 2", "aquasecurity/trivy-action", "want exactly 1") {
		t.Errorf("error should say the step count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_HardcodedSkipDirsFailsLoudly pins the core new
// assertion: a hard-coded skip-dirs literal (no reference to any step output
// at all) is drift, since it can no longer be tied back to the specs file.
func TestTrivySkipDirsParity_HardcodedSkipDirsFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: tests/fixtures,examples\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when skip-dirs is a hard-coded literal")
	}
	if !containsAll(got, "is not exactly a", `"${{ steps.<id>.outputs.<name> }}"`) {
		t.Errorf("error should say the input is not a bare steps-output expression, got: %s", got)
	}
}

// TestTrivySkipDirsParity_AppendedSkipDirsFailsLoudly pins that exact string
// equality on the skip-dirs input kills the append-at-the-call-site bypass
// (#5925 F-B) for free: a value that CONTAINS the expression but has extra
// text appended is not exactly the expression.
func TestTrivySkipDirsParity_AppendedSkipDirsFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }},node_modules\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the skip-dirs expression has an appended literal")
	}
	if !containsAll(got, "is not exactly a") {
		t.Errorf("error should say the input is not a bare steps-output expression, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepMissingFailsLoudly pins that the
// skip-dirs expression referencing a step id that does not exist in the
// trivy-fs job is drift, not silently ignored.
func TestTrivySkipDirsParity_ProducerStepMissingFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the referenced producer step does not exist")
	}
	if !containsAll(got, "0 step(s) with id", `"skipdirs"`, "want exactly 1") {
		t.Errorf("error should say the producer step count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepAmbiguousFailsLoudly pins the other
// arity failure: two steps sharing the referenced id.
func TestTrivySkipDirsParity_ProducerStepAmbiguousFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: First\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - name: Second\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when two steps share the referenced producer id")
	}
	if !containsAll(got, "2 step(s) with id", `"skipdirs"`, "want exactly 1") {
		t.Errorf("error should say the producer step count is wrong, got: %s", got)
	}
}

// #5927 review: checkCIWorkflowSkipDirsFromHelper used to treat ANY
// occurrence of scripts/lib/trivy-skip-dirs.sh in the producer step's run:
// block as "invoking" the helper (strings.Contains), so a mention of the
// path in a string literal or an echo -- while the step emitted a
// hard-coded `dirs=...` -- read as wired. The producer step must now prove
// both halves of a real invocation: a `source` (or `.`) line that loads the
// helper's definitions, AND a call to the function it defines,
// trivy_skip_dirs_csv (checkSourcedAndCalled). The tests below replace
// TestTrivySkipDirsParity_ProducerStepDoesNotInvokeHelperFailsLoudly and
// TestTrivySkipDirsParity_ProducerStepMentionsHelperOnlyInCommentFailsLoudly
// with that finer-grained assertion.

// TestTrivySkipDirsParity_ProducerStepDoesNotSourceHelperFailsLoudly pins
// that the producer step must actually invoke the helper, not merely have
// the right id and produce SOME output.
func TestTrivySkipDirsParity_ProducerStepDoesNotSourceHelperFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          echo \"dirs=tests/fixtures,examples\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the producer step's run: block does not source the helper")
	}
	if !containsAll(got, "does not `source`", "trivy-skip-dirs.sh") {
		t.Errorf("error should say the producer step does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepMentionsHelperOnlyInCommentFailsLoudly
// pins #5925 F3: the producer's run: block is stripped of WHOLE comment lines
// before the "does it source the helper" question is asked, symmetric with
// how the local-script and helper-script checks already treat comments. A
// step whose ONLY mention of the helper path is a comment line, with the
// actual emitted value hard-coded, must not read as "invokes the helper".
func TestTrivySkipDirsParity_ProducerStepMentionsHelperOnlyInCommentFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          # scripts/lib/trivy-skip-dirs.sh\n" +
		"          echo \"dirs=node_modules\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the only mention of the helper path is a whole-line comment")
	}
	if !containsAll(got, "does not `source`", "trivy-skip-dirs.sh") {
		t.Errorf("error should say the producer step does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepMentionsHelperInEchoFailsLoudly pins
// the exact false-green Copilot flagged on #5927: a NON-comment mention of
// the helper's path -- here inside an echo string that is not a `source`
// line -- must not read as "invokes the helper" while a hard-coded value
// governs the actual emitted skip-dirs.
func TestTrivySkipDirsParity_ProducerStepMentionsHelperInEchoFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          echo \"would use scripts/lib/trivy-skip-dirs.sh\"\n" +
		"          echo \"dirs=tests/fixtures,examples\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the only mention of the helper's path is inside an echo string")
	}
	if !containsAll(got, "does not `source`", "trivy-skip-dirs.sh") {
		t.Errorf("error should say the producer step does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepSourcesHelperButNeverCallsFailsLoudly
// pins the other half: sourcing the helper alone does not run its
// derivation -- the producer step must also call trivy_skip_dirs_csv.
func TestTrivySkipDirsParity_ProducerStepSourcesHelperButNeverCallsFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          echo \"dirs=tests/fixtures,examples\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the producer step sources the helper but never calls trivy_skip_dirs_csv")
	}
	if !containsAll(got, "never calls trivy_skip_dirs_csv", "not wired") {
		t.Errorf("error should say the producer step never calls trivy_skip_dirs_csv, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepCallsHelperButNeverSourcesFailsLoudly
// pins the symmetric gap: calling trivy_skip_dirs_csv without first sourcing
// the file that defines it cannot possibly be the real, governing call.
func TestTrivySkipDirsParity_ProducerStepCallsHelperButNeverSourcesFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the producer step calls trivy_skip_dirs_csv without sourcing the helper")
	}
	if !containsAll(got, "does not `source`", "trivy-skip-dirs.sh") {
		t.Errorf("error should say the producer step does not source the helper, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepCallsHelperTwiceFailsLoudly pins #5925
// (single-source review) F4's CI-producer twin: the call-arity ">1" branch
// in checkSourcedAndCalled had no test on either side of this package.
// Replacing that branch with `return nil` would leave every other test in
// this package green while silently accepting a producer step that calls
// trivy_skip_dirs_csv more than once.
func TestTrivySkipDirsParity_ProducerStepCallsHelperTwiceFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        shell: bash\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the producer step calls trivy_skip_dirs_csv twice")
	}
	if !containsAll(got, "calls trivy_skip_dirs_csv", "2 time(s)", "want exactly 1") {
		t.Errorf("error should say the call count is wrong, got: %s", got)
	}
}

// TestTrivySkipDirsParity_ProducerStepMissingShellBashFailsLoudly pins #5925
// (single-source review) F7: the producer step's `run:` block only gets
// bash's `-eo pipefail` -- which makes an unreadable specs file inside
// trivy_skip_dirs_csv's pipeline fail the step instead of silently emitting
// an empty skip-dirs value -- when the step declares `shell: bash` (GitHub
// Actions' default run shell omits pipefail). A step that otherwise sources
// and calls the helper correctly but never declares `shell: bash` must still
// fail loudly, not read as fully wired.
func TestTrivySkipDirsParity_ProducerStepMissingShellBashFailsLoudly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := "name: Security Scan\non: [push]\njobs:\n  trivy-fs:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Read skip-dirs\n        id: skipdirs\n        run: |\n" +
		"          source scripts/lib/trivy-skip-dirs.sh\n" +
		"          dirs=\"$(trivy_skip_dirs_csv .)\"\n" +
		"          echo \"dirs=${dirs}\" >> \"$GITHUB_OUTPUT\"\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: ${{ steps.skipdirs.outputs.dirs }}\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	got := driftFor(root)
	if got == "" {
		t.Fatal("expected an error when the producer step does not declare shell: bash")
	}
	if !containsAll(got, "shell: bash") {
		t.Errorf("error should say the step does not declare shell: bash, got: %s", got)
	}
}

// TestTrivySkipDirsParity_UnrelatedJobIgnored pins that an unrelated job in
// the same workflow -- even one with its own skip-dirs or step ids -- cannot
// mask or be mistaken for the trivy-fs job's own wiring, because the CI-side
// check is scoped to the trivy-fs job specifically.
func TestTrivySkipDirsParity_UnrelatedJobIgnored(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	body := validWorkflowBody() +
		"  other-job:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: aquasecurity/trivy-action@master\n        with:\n" +
		"          skip-dirs: totally-different-set\n"
	writeTrivyArtifacts(t, root, validSpecsBody, validHelperScriptBody(), validLocalScriptBody(), body)

	if got := driftFor(root); got != "" {
		t.Errorf("an unrelated job must not affect a correctly wired trivy-fs job, got: %s", got)
	}
}
