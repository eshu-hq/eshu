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

// prewarmOrderingErrs runs DriftCheck and returns only this check's own
// findings (the ones naming "warms that module"), the same isolation
// scriptTriggerErrs (scripttrigger_test.go) uses for check 8/11 -- a case
// asserts on its own rule, not on whatever else a minimal fixture repo trips
// in the other ~12 DriftCheck rules.
func prewarmOrderingErrs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, minimalReg(nil, nil, nil)) {
		msg := err.Error()
		if strings.Contains(msg, "warms that module") {
			out = append(out, msg)
		}
	}
	return out
}

// buildPrewarmRepo is buildDriftRepo (drift_test.go) with the workflow body
// supplied directly, rather than a fixed stub -- this check's cases need
// specific step orderings the shared stub cannot express.
func buildPrewarmRepo(t *testing.T, workflowName, workflowBody string) string {
	t.Helper()
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pre-commit-config.yaml"), []byte(minimalPreCommitHygiene()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, workflowName), []byte(workflowBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestCheckSetupGoPrewarmOrdering_NoPrewarmAtAll_Violation is the RED case:
// a job with setup-go caching go/go.sum runs `go build` with no pre-warm
// step anywhere. This is the shape #6615 exists to close.
func TestCheckSetupGoPrewarmOrdering_NoPrewarmAtAll_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("no-prewarm-at-all fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_PrewarmAfterTouch_Violation is the second
// RED case, and the one the review specifically asked for: a pre-warm step
// exists in the job, but AFTER the Go-touching step, so it warms the cache
// too late to help that step. A count-only check (does the string appear
// anywhere in the job) would wrongly pass this; an order-aware check must
// not.
func TestCheckSetupGoPrewarmOrdering_PrewarmAfterTouch_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Build
        run: go build ./...
      - name: Pre-warm Go modules
        run: scripts/ci/go-mod-download-retry.sh
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("prewarm-after-touch fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_WrongModule_Violation proves the pre-warm
// step must warm the SAME module setup-go's cache-dependency-path names, not
// merely appear before the touch: the bare (go/-defaulting) form does not
// satisfy a setup-go step caching a different module's go.mod.
func TestCheckSetupGoPrewarmOrdering_WrongModule_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: sdk/go/collector/go.mod
          cache-dependency-path: sdk/go/collector/go.mod
      - name: Pre-warm Go modules
        run: scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("wrong-module fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_Clean_NoError is the GREEN fixture case:
// the pre-warm step appears, with the right module argument, before the
// touch. Mirrors the shape #6615 actually ships in every real job.
func TestCheckSetupGoPrewarmOrdering_Clean_NoError(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: sdk/go/collector/go.mod
          cache-dependency-path: sdk/go/collector/go.mod
      - name: Pre-warm Go modules
        run: scripts/ci/go-mod-download-retry.sh sdk/go/collector
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("clean fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_NoGoTouchNoPrewarm_Violation proves the F2
// "must warm regardless of touch" invariant directly: a cached setup-go step
// followed only by a harmless `go version` (which matches no touch pattern)
// and NO pre-warm is still a violation -- this is the review's blocking-gap
// shape restated with a literal `go` command that nonetheless never touches
// the module graph, so detecting a touch can never be what decides this
// rule. (Superseded the old NoGoTouch_NoError case, whose premise -- "no
// touch detected" implies "nothing to warm" -- was the bug.)
func TestCheckSetupGoPrewarmOrdering_NoGoTouchNoPrewarm_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Just go version, not a module touch
        run: go version
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("no-go-touch-no-prewarm fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_ModuleToolTouch_Violation proves
// golangci-lint/govulncheck/gosec/nancy count as a module-graph touch too,
// not only a literal `go <verb>` -- the security-scan.yml and go-core shape
// #6615's audit found.
func TestCheckSetupGoPrewarmOrdering_ModuleToolTouch_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Run govulncheck
        run: govulncheck -scan package ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("module-tool-touch fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_NonGoStepNoPrewarm_Violation is the review's
// blocking-gap case: a job with a cached setup-go step is followed only by a
// step that is NOT a literal `go <verb>` or module-touching tool (a plain
// script invocation), and never runs a pre-warm at all. The bug this closes:
// findPrewarmOrderingViolation used to return "" whenever nothing matched
// goTouchRE/moduleTouchingToolRE, which is exactly the shape of the job that
// caused #6615's root cause (102442878753, "Verify factschema-diff test
// mirror" -- a fast job whose only Go execution happens inside a script, not
// as a literal verb in the workflow step).
func TestCheckSetupGoPrewarmOrdering_NonGoStepNoPrewarm_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Run a non-Go script
        run: bash scripts/foo.sh
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("non-Go-step-no-prewarm fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_NonGoStepWithPrewarm_Clean is (a)'s clean
// counterpart: the same non-Go step, but the job also runs a matching
// pre-warm -- required now even though nothing else in the job would have
// tripped goTouchRE.
func TestCheckSetupGoPrewarmOrdering_NonGoStepWithPrewarm_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm Go modules
        run: scripts/ci/go-mod-download-retry.sh
      - name: Run a non-Go script
        run: bash scripts/foo.sh
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("non-Go-step-with-prewarm fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_CacheDisabled_Clean proves an explicit
// `cache: false` setup-go step is exempt: it restores and saves nothing, so
// there is no cache-race invariant to enforce. actions/setup-go's own
// action.yml (v5 and v6, fetched directly from
// raw.githubusercontent.com/actions/setup-go/<ref>/action.yml) declares
// `cache: { default: true }`, so a step with NO cache: key is cached by
// default -- only an explicit `false` exempts it, which is why every other
// case in this file omits the key and still expects the rule to apply.
func TestCheckSetupGoPrewarmOrdering_CacheDisabled_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
          cache: false
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("cache-disabled fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_UnparseableWorkflow_Error proves a
// workflow file this check cannot read or parse fails closed (reports an
// error naming the file) rather than silently skipping it -- the review's
// second finding: the prior version's `continue` on a read/parse failure
// hid a broken workflow from this check entirely.
func TestCheckSetupGoPrewarmOrdering_UnparseableWorkflow_Error(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", "not: [valid: yaml: at: all\n")

	var got []string
	for _, err := range cigates.DriftCheck(root, minimalReg(nil, nil, nil)) {
		msg := err.Error()
		if strings.Contains(msg, "fixture.yml") && strings.Contains(msg, "parse") {
			got = append(got, msg)
		}
	}
	if len(got) != 1 {
		t.Fatalf("unparseable-workflow fixture: got %d parse-error findings naming fixture.yml, want 1 (all errs: %v)",
			len(got), driftCheckErrStrings(t, root))
	}
}

// driftCheckErrStrings is a debugging helper for a failed
// TestCheckSetupGoPrewarmOrdering_UnparseableWorkflow_Error assertion: it
// prints every DriftCheck error so a mismatch is diagnosable from the
// failure output alone.
func driftCheckErrStrings(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, minimalReg(nil, nil, nil)) {
		out = append(out, err.Error())
	}
	return out
}

// TestCheckSetupGoPrewarmOrdering_CommittedWorkflows_NoError is the live
// GREEN proof: every real job in every committed .github/workflows/*.yml
// file satisfies this rule today. Uses the real repo root, not a fixture --
// the property under test is about the committed workflows, and a fixture
// would pass while a real one drifted.
func TestCheckSetupGoPrewarmOrdering_CommittedWorkflows_NoError(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	var got []string
	for _, err := range cigates.DriftCheck(repoRoot, mustLoadCommittedRegistryForPrewarmTest(t, repoRoot)) {
		msg := err.Error()
		if strings.Contains(msg, "warms that module") {
			got = append(got, msg)
		}
	}
	if len(got) != 0 {
		t.Fatalf("committed workflows: got %d prewarm-ordering violations, want 0:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// mustLoadCommittedRegistryForPrewarmTest loads the real specs/ci-gates.v1.yaml.
// A separate small helper rather than reusing loadCommittedRegistry
// (valueflow_required_test.go) because that helper also returns a repoRoot
// string this test already has and does not need duplicated.
func mustLoadCommittedRegistryForPrewarmTest(t *testing.T, repoRoot string) *cigates.Registry {
	t.Helper()
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}
	return reg
}
