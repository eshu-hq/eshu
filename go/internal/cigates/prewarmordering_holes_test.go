// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// Split from prewarmordering_test.go (500-line cap): this file holds the
// round-2 review's blocking-hole cases 1-3, plus the live committed-tree
// GREEN check. Shares buildPrewarmRepo/prewarmOrderingErrs with that file --
// same package, same directory.

// TestCheckSetupGoPrewarmOrdering_UnresolvableCacheDependencyPath_Violation
// is hole 1: cache-dependency-path is an unresolved matrix expression, so
// prewarmModuleDirFromCacheDependencyPath could not previously derive a
// module -- and the caller silently skipped the whole setup-go step, exactly
// the shape a matrix-dispatched cache key would have. Must fail closed
// instead.
func TestCheckSetupGoPrewarmOrdering_UnresolvableCacheDependencyPath_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    strategy:
      matrix:
        sum: ["go/go.sum"]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: ${{ matrix.sum }}
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("unresolvable-cache-dependency-path fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "cannot be resolved") {
		t.Fatalf("unresolvable-cache-dependency-path fixture: message %q does not say the path is unresolvable", got[0])
	}
}

// TestCheckSetupGoPrewarmOrdering_MissingCacheDependencyPath_Violation is
// hole 1's other shape: cache is enabled (the default) but
// cache-dependency-path is absent entirely. actions/setup-go's own README
// (fetched from raw.githubusercontent.com/actions/setup-go/<ref>/README.md
// for v5 and v6) says: "By default, the action looks for go.mod in the
// repository root". This repo's go.mod lives under go/, not at the
// repository root, so that default cannot be mapped to a real module here --
// this must fail closed rather than silently exempt the step, the same as
// an unresolved expression.
func TestCheckSetupGoPrewarmOrdering_MissingCacheDependencyPath_Violation(t *testing.T) {
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
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("missing-cache-dependency-path fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "cannot be resolved") {
		t.Fatalf("missing-cache-dependency-path fixture: message %q does not say the path is unresolvable", got[0])
	}
}

// TestCheckSetupGoPrewarmOrdering_SameLineTouchBeforePrewarm_Violation is
// hole 2: a single multi-line `run:` block runs `go build ./...` on one
// line and the matching pre-warm on a LATER line of the SAME step. The bug:
// finding the pre-warm match anywhere in step.Run short-circuited into
// "prewarm found", without checking whether the text preceding that match
// already touched the module graph.
func TestCheckSetupGoPrewarmOrdering_SameLineTouchBeforePrewarm_Violation(t *testing.T) {
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
      - name: Build then (too-late) pre-warm
        run: |
          go build ./...
          scripts/ci/go-mod-download-retry.sh
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("same-step-touch-before-prewarm fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_SameLinePrewarmBeforeTouch_Clean is (2)'s
// clean counterpart: the pre-warm line comes first in the same multi-line
// step, so the touch on the later line is fine.
func TestCheckSetupGoPrewarmOrdering_SameLinePrewarmBeforeTouch_Clean(t *testing.T) {
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
      - name: Pre-warm then build
        run: |
          scripts/ci/go-mod-download-retry.sh
          go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("same-step-prewarm-before-touch fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_ExitStatusSuppressed_Violation is hole 3:
// the pre-warm command's own exit status is thrown away with `|| true`, so
// a genuinely failing proxy warm-up (all retries exhausted) no longer fails
// the job -- defeating scripts/ci/go-mod-download-retry.sh's whole "fail
// HERE, loud" purpose. The command still runs, but this check must not
// credit it as a real safety net.
func TestCheckSetupGoPrewarmOrdering_ExitStatusSuppressed_Violation(t *testing.T) {
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
      - name: Pre-warm (but swallow failure)
        run: scripts/ci/go-mod-download-retry.sh || true
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("exit-status-suppressed fixture: got %d violations, want 1: %v", len(got), got)
	}
	// Content-checked, not just a count: the pre-fix regex can accidentally
	// capture "||" as a bogus module-dir argument (a different bug) and
	// still land on 1 violation for the wrong reason -- via the unrelated
	// "go build" step below never getting a prewarm. Requiring the message
	// name suppression specifically closes that false-RED-for-wrong-reason
	// gap.
	if !strings.Contains(got[0], "suppress") {
		t.Fatalf("exit-status-suppressed fixture: message %q does not name suppression", got[0])
	}
}

// TestCheckSetupGoPrewarmOrdering_ContinueOnError_Violation is hole 3's
// step-level shape: continue-on-error: true means the step can fail (all
// retries exhausted) and the job still proceeds green.
func TestCheckSetupGoPrewarmOrdering_ContinueOnError_Violation(t *testing.T) {
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
      - name: Pre-warm
        continue-on-error: true
        run: scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("continue-on-error fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_StepIfFalse_Violation is hole 3's other
// step-level shape: a literal `if: false` (or the equivalent
// `if: ${{ false }}`) means the pre-warm step never runs at all.
func TestCheckSetupGoPrewarmOrdering_StepIfFalse_Violation(t *testing.T) {
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
      - name: Pre-warm
        if: ${{ false }}
        run: scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("step-if-false fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_QuotedModuleArg_Clean proves the
// acceptable false-fail direction named in review is closed cheaply: a
// quoted module argument (`go-mod-download-retry.sh "sdk/go/collector"`)
// still matches its unquoted cache-dependency-path-derived module.
func TestCheckSetupGoPrewarmOrdering_QuotedModuleArg_Clean(t *testing.T) {
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
        run: scripts/ci/go-mod-download-retry.sh "sdk/go/collector"
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("quoted-module-arg fixture: got %d unexpected violations: %v", len(got), got)
	}
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
		if strings.Contains(msg, "actions/setup-go step") {
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
