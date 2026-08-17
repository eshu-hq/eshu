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

// writeScript creates repoRoot/rel with body, making parent directories.
func writeScript(t *testing.T, repoRoot, rel, body string) {
	t.Helper()
	p := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scriptTriggerErrs runs DriftCheck and returns only the check-8 findings, so a
// case asserts on its own rule rather than on whatever else the fixture repo
// happens to trip.
func scriptTriggerErrs(t *testing.T, root string, reg *cigates.Registry) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, reg) {
		msg := err.Error()
		if strings.Contains(msg, "no trigger of that gate matches it") {
			out = append(out, msg)
		}
	}
	return out
}

// hiddenCdScriptErrs runs DriftCheck and returns only the cd-prefixed
// hidden-scripts-token findings (hiddenScriptsInCdPrefixedCommand in
// scripttrigger.go), so a case asserts on its own rule rather than the
// "no trigger of that gate matches it" class scriptTriggerErrs isolates.
func hiddenCdScriptErrs(t *testing.T, root string, reg *cigates.Registry) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, reg) {
		msg := err.Error()
		if strings.Contains(msg, "cd-prefixed and tokenizes to") {
			out = append(out, msg)
		}
	}
	return out
}

// A gate whose verifier is not covered by its own triggers is the shape that
// let `make pre-pr` print "SKIPPED openapi-surface — no trigger matched changed
// paths" for a PR that edited only scripts/verify-openapi.sh (#5762 round 8,
// P1-3).
func TestDriftCheck_GateScriptNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**"}
	g.Local = &cigates.Local{Command: "bash scripts/verify-thing.sh"}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 script-trigger error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "scripts/verify-thing.sh") || !strings.Contains(errs[0], "local.command") {
		t.Errorf("error should name the field and the script, got: %s", errs[0])
	}
}

// The same gate goes clean once the script is among its triggers.
func TestDriftCheck_GateScriptCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/test-verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/*verify-thing*.sh"}
	g.Local = &cigates.Local{
		Command:     "bash scripts/verify-thing.sh",
		TestCommand: "bash scripts/test-verify-thing.sh",
	}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("expected no script-trigger errors, got %d: %v", len(errs), errs)
	}
}

// A case file the test mirror sources is as much a part of the gate as the
// mirror itself: narrowing the trigger from a glob back to one filename leaves
// the sibling case file unselected, which is what mutant NS5 does to the
// openapi-surface gate.
func TestDriftCheck_SourcedCaseFileNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/test-verify-thing.sh",
		"#!/usr/bin/env bash\n"+
			"# shellcheck source=scripts/lib/thing-b-cases.sh\n"+
			". \"${repo_root}/scripts/lib/thing-a-cases.sh\"\n"+
			"source \"${repo_root}/scripts/lib/thing-b-cases.sh\"\n")
	writeScript(t, root, "scripts/lib/thing-a-cases.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/lib/thing-b-cases.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{
		"go/**",
		"scripts/*verify-thing*.sh",
		"scripts/lib/thing-a-cases.sh",
	}
	g.Local = &cigates.Local{
		Command:     "bash scripts/verify-thing.sh",
		TestCommand: "bash scripts/test-verify-thing.sh",
	}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 sourced-file error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], `sources "scripts/lib/thing-b-cases.sh"`) {
		t.Errorf("error should name the uncovered sourced file, got: %s", errs[0])
	}
	if strings.Contains(errs[0], `sources "scripts/lib/thing-a-cases.sh"`) {
		t.Errorf("the covered sourced file must not be reported, got: %s", errs[0])
	}
}

// A file reached only by sourcing a sourced file (level 2) is the
// golden-corpus-lock-cases.sh shape in the real registry:
// scripts/test-verify-golden-corpus-gate.sh sources it directly, and it in
// turn sources golden-corpus-lock-parse-cases.sh and
// golden-corpus-lock-race-cases.sh. Before the walk below followed sourcing
// transitively, a level-2 file was invisible to check 8 even though editing
// it alone could leave the gate unselected (#5762 round 9, P1-1).
func TestDriftCheck_TransitivelySourcedFileNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/test-verify-thing.sh",
		"#!/usr/bin/env bash\n"+
			". \"${repo_root}/scripts/lib/thing-level1-cases.sh\"\n")
	writeScript(t, root, "scripts/lib/thing-level1-cases.sh",
		"#!/usr/bin/env bash\n"+
			". \"${repo_root}/scripts/lib/thing-level2-cases.sh\"\n")
	writeScript(t, root, "scripts/lib/thing-level2-cases.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{
		"go/**",
		"scripts/*verify-thing*.sh",
		"scripts/lib/thing-level1-cases.sh",
		// scripts/lib/thing-level2-cases.sh is deliberately absent — it is
		// only reachable by sourcing the level-1 file.
	}
	g.Local = &cigates.Local{
		Command:     "bash scripts/verify-thing.sh",
		TestCommand: "bash scripts/test-verify-thing.sh",
	}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 transitively-sourced-file error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], `sources "scripts/lib/thing-level2-cases.sh"`) {
		t.Errorf("error should name the uncovered level-2 file, got: %s", errs[0])
	}
}

// A gate whose local command chains more than one scripts/ invocation with
// "&&" is the ci-gate-registry / frontend-console-checks shape in the real
// registry. Before the fix, check 8 derived only the leading scripts/ token
// from a command, so a second (or third, or fourth) chained script was
// invisible to the coverage check (#5762 round 9, P2-2).
func TestDriftCheck_SecondScriptInCompoundCommandNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/test-verify-thing-a.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/test-verify-thing-b.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{
		"go/**",
		"scripts/test-verify-thing-a.sh",
		// scripts/test-verify-thing-b.sh is deliberately absent — it is the
		// second scripts/ token in a chained local.command.
	}
	g.Local = &cigates.Local{
		Command: "bash scripts/test-verify-thing-a.sh && bash scripts/test-verify-thing-b.sh",
	}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 second-script error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], `runs "scripts/test-verify-thing-b.sh"`) {
		t.Errorf("error should name the uncovered second script, got: %s", errs[0])
	}
}

// The `# shellcheck source=` directive on its own must not count as a
// dependency: the fixture below sources nothing, so a gate that lists only its
// two scripts is clean.
func TestDriftCheck_ShellcheckDirectiveIsNotASourcedFile(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh",
		"#!/usr/bin/env bash\n# shellcheck source=scripts/lib/never-sourced.sh\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/verify-thing.sh"}
	g.Local = &cigates.Local{Command: "bash scripts/verify-thing.sh"}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("expected no script-trigger errors, got %d: %v", len(errs), errs)
	}
}

// An inline toolchain command references no file whose edit could go
// unselected, so it is skipped rather than reported.
func TestDriftCheck_InlineToolchainCommandSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**"}
	g.Local = &cigates.Local{Command: "cd go && go test ./... -count=1"}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("expected no script-trigger errors for an inline command, got %d: %v", len(errs), errs)
	}
	if errs := hiddenCdScriptErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("expected no hidden-cd-script errors for a cd-prefixed command with no scripts/ token, got %d: %v", len(errs), errs)
	}
}

// sourcedScripts only resolves a `source`/`.` line whose LITERAL TEXT
// contains "scripts/" (#5762 round 10, P2-1). A line that sources a computed
// path through a variable — the shape scripts/verify-maturity-drift-guard.sh
// and five other gate entry scripts use, 11 such lines in all, enumerated
// with their gates in checkScriptTriggerCoverage's doc comment — is invisible
// to the walk, at any depth. "Entry script" there means the file a gate's
// local.command or local.test_command names directly; counting the case libs
// those transitively source raises the population to the 37 lines the same
// doc comment measures. This pins that boundary:
// scripts/lib/thing-variable-cases.sh is a
// real, uncovered file the command reaches, but check 8 reports 0 errors
// because it never resolves the variable to discover the file.
func TestDriftCheck_VariableSourcedFileNotResolved(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh",
		"#!/usr/bin/env bash\n"+
			"script_dir=\"$(cd \"$(dirname \"${BASH_SOURCE[0]}\")\" && pwd)\"\n"+
			". \"${script_dir}/lib/thing-variable-cases.sh\"\n")
	writeScript(t, root, "scripts/lib/thing-variable-cases.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/verify-thing.sh"}
	// scripts/lib/thing-variable-cases.sh is deliberately absent from
	// triggers. Check 8 must not flag it: it cannot see past the
	// variable-sourced line to learn the file exists.
	g.Local = &cigates.Local{Command: "bash scripts/verify-thing.sh"}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("variable-sourced file must not be discovered by the literal-text-only walk, got %d errors: %v", len(errs), errs)
	}
}

// extractScriptPaths returns nil for any command starting with "cd ", BEFORE
// it tokenizes anything — so a cd-prefixed command that does carry a
// scripts/ token later in the pipeline used to be exempted from check 8
// exactly like one that carries none (#5762 round 10, P2-2). #5934 review
// flagged that silence as a trapdoor: nothing stopped a future
// "cd apps/console && bash scripts/x.sh" from landing unnoticed just because
// today's registry happens to have none. check 8 now reports that token as
// its own loud finding via hiddenScriptsInCdPrefixedCommand instead of
// silently reproducing extractScriptPaths' skip. This pins the new
// reporting: scripts/uncovered.sh is a real, uncovered file the command
// names, and check 8 now reports it by name.
func TestDriftCheck_CdPrefixedCommandWithScriptsTokenReported(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/uncovered.sh", "#!/usr/bin/env bash\ntrue\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**"} // scripts/uncovered.sh is deliberately absent
	g.Local = &cigates.Local{Command: "cd apps/console && bash scripts/uncovered.sh"}

	errs := hiddenCdScriptErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 hidden cd-prefixed script-token error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "scripts/uncovered.sh") {
		t.Errorf("error should name the hidden script, got: %s", errs[0])
	}
}

// writeWorkflowWithRun creates repoRoot/.github/workflows/name with a single
// job keyed jobKey (also used as its display name, so gateWith's default
// ci.job value "job" resolves), running each of commands as its own `run:`
// step in order.
func writeWorkflowWithRun(t *testing.T, root, name, jobKey string, commands []string) {
	t.Helper()
	var steps strings.Builder
	for _, c := range commands {
		steps.WriteString("      - run: ")
		steps.WriteString(c)
		steps.WriteString("\n")
	}
	body := "name: test\non: [push]\njobs:\n  " + jobKey + ":\n    name: " + jobKey +
		"\n    runs-on: ubuntu-latest\n    steps:\n" + steps.String()
	p := filepath.Join(root, ".github", "workflows", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The script CI actually executes for a gate is routinely a DIFFERENT file
// than local.command: ifa-determinism's CI job runs the live driver
// scripts/verify-ifa-determinism.sh while local.command runs the static
// mirror scripts/test-verify-ifa-determinism.sh. Check 8 only ever walked
// local.command/local.test_command, so a scripts/ token that appears ONLY in
// the CI job's own run: command was invisible to it -- a PR editing only
// that script would pass locally and fail first in CI, the exact gap check 8
// exists to close for local.command (#6149 follow-up item 2).
func TestDriftCheck_CIJobScriptNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	writeWorkflowWithRun(t, root, "verify.yml", "job", []string{"bash scripts/verify-thing.sh"})

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**"} // scripts/verify-thing.sh deliberately absent
	// g.Local stays gateWith's neutral inline command, so check 8's OWN
	// local.command walk contributes nothing here -- only the new CI-job walk
	// should trip.

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 CI-job script-trigger error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "scripts/verify-thing.sh") || !strings.Contains(errs[0], "ci job") {
		t.Errorf("error should name the CI job and the script, got: %s", errs[0])
	}
}

// The same gate goes clean once the CI-run script is among its triggers.
func TestDriftCheck_CIJobScriptCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	writeWorkflowWithRun(t, root, "verify.yml", "job", []string{"bash scripts/verify-thing.sh"})

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/verify-thing.sh"}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("expected no CI-job script-trigger errors, got %d: %v", len(errs), errs)
	}
}

// The shape #6149 hit for real: a gate's CI job runs a "live driver" script
// that SOURCES per-family case libraries, while local.command runs a static
// mirror that never sources them at all (scripts/verify-ifa-determinism.sh
// vs. scripts/test-verify-ifa-determinism.sh). Three cap-driven split files
// shipped with no trigger row on that branch and had to be caught by hand,
// because nothing walked the CI job's own sourcing graph. This fixture pins
// that the CI-job walk follows sourcing exactly like the local.command walk
// does.
func TestDriftCheck_CIJobSourcedScriptNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeScript(t, root, "scripts/verify-thing.sh",
		"#!/usr/bin/env bash\nsource \"${repo_root}/scripts/lib/thing-live-cases.sh\"\n")
	writeScript(t, root, "scripts/lib/thing-live-cases.sh", "#!/usr/bin/env bash\ntrue\n")
	writeWorkflowWithRun(t, root, "verify.yml", "job", []string{"bash scripts/verify-thing.sh"})

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/verify-thing.sh"}
	// scripts/lib/thing-live-cases.sh is deliberately absent from triggers.

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 CI-job sourced-file error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], `sources "scripts/lib/thing-live-cases.sh"`) {
		t.Errorf("error should name the uncovered sourced file, got: %s", errs[0])
	}
}

// A CI job shared by more than one gate (the real registry's
// test.yml/verify-contracts shape: 11 gates all declare that same ci.job as
// a backstop job that runs roughly thirty unrelated gates' verify scripts as
// separate steps) carries no per-gate attribution signal. A first version of
// the CI-job walk attributed every run: step in a resolved job to whichever
// gate named it and produced 352 false positives against the committed
// registry from exactly this shape. This fixture pins that a shared
// (workflow, job) pair is skipped entirely, for BOTH gates that share it --
// even though gate-a's own uncovered script is really there and gate-b's
// covered one is really there too, neither should be reported, because
// nothing here can tell which step belongs to which gate.
func TestDriftCheck_CIJobSharedByMultipleGatesSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeScript(t, root, "scripts/verify-a.sh", "#!/usr/bin/env bash\ntrue\n")
	writeScript(t, root, "scripts/verify-b.sh", "#!/usr/bin/env bash\ntrue\n")
	writeWorkflowWithRun(t, root, "shared.yml", "shared-job", []string{
		"bash scripts/verify-a.sh",
		"bash scripts/verify-b.sh",
	})

	a := gateWith("gate-a", "gate-a", "shared.yml")
	a.Triggers = []string{"go/**"} // scripts/verify-a.sh deliberately absent
	b := gateWith("gate-b", "gate-b", "shared.yml")
	b.Triggers = []string{"go/**", "scripts/verify-b.sh"}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{a, b}, nil, nil))
	if len(errs) != 0 {
		t.Errorf("expected zero CI-job script-trigger errors for a shared job, got %d: %v", len(errs), errs)
	}
}
