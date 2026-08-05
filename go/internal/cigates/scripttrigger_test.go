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
}
