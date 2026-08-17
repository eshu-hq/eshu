// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Load's local: block rules, split out of load_test.go: adding
// TestLoad_LocalBlockWithNeitherCommandRejected and
// TestLoad_LocalBlockWithOnlyTestCommandAccepted (#6149 follow-up item 8
// review, P1) pushed that file to 527 lines against the repository's
// 500-line cap.

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestLoad_LocalNullWithoutReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: ci-only-no-reason
    name: CI Only No Reason
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for local==null without ci_only_reason, got nil")
	}
}

func TestLoad_LocalNullWithReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: ci-only-with-reason
    name: CI Only With Reason
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: "needs Postgres service"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if reg.Gates[0].Local != nil {
		t.Error("expected Local to be nil for CI-only gate")
	}
	if reg.Gates[0].CIOnlyReason != "needs Postgres service" {
		t.Errorf("CIOnlyReason = %q", reg.Gates[0].CIOnlyReason)
	}
}

func TestLoad_LocalOnlyReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: local-only-proof
    name: Local Only Proof
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["specs/local-proof.v1.yaml"]
    local:
      command: "bash scripts/verify-local-proof.sh"
    ci:
      workflow: ""
      job: ""
    requirements: [go]
    ci_only_reason: ""
    local_only_reason: "review-only local fixture until CI has an equivalent runner"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := reg.Gates[0].LocalOnlyReason; got != "review-only local fixture until CI has an equivalent runner" {
		t.Errorf("LocalOnlyReason = %q", got)
	}
}

// TestLoad_LocalBlockWithNeitherCommandRejected proves a gate that declares a
// local: block with both command and test_command empty is a registry error,
// not a silent no-op. Neither of the two existing Load rules (ci_only_reason
// required when local is absent; local_only_reason required when
// blocking:false has no CI backstop) rejects this shape -- a non-nil local
// with nothing in it satisfies both. Before this rule, ci-gates run's
// executeGates would print "PASS <gate>" for a gate that never ran a single
// command, indistinguishable from a gate that actually ran and passed
// (#6149 follow-up item 8 review, P1: the runner-side fix alone left this
// registry-authored path unguarded).
func TestLoad_LocalBlockWithNeitherCommandRejected(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: empty-local-block
    name: Empty Local Block
    category: hygiene
    tier: pre-pr
    blocking: false
    triggers: ["go/**"]
    local:
      command: ""
      test_command: ""
    ci:
      workflow: ""
      job: ""
    requirements: []
    ci_only_reason: ""
    local_only_reason: "declared, but the local block itself has nothing to run"
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for a local block with neither command nor test_command, got nil")
	}
	if !strings.Contains(err.Error(), "empty-local-block") {
		t.Errorf("error %q should name the gate", err.Error())
	}
}

// TestLoad_LocalBlockWithOnlyTestCommandAccepted proves the new rule does not
// reject the real, committed shape it must coexist with: a gate whose
// command is intentionally empty but whose test_command is real
// (prepr-stamp-verify-selftest's exact shape -- its guard cannot run as a
// local.command at all, see specs/ci-gates.v1.yaml's own local_only_reason
// for why).
func TestLoad_LocalBlockWithOnlyTestCommandAccepted(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: selftest-only-gate
    name: Selftest Only Gate
    category: hygiene
    tier: pre-pr
    blocking: false
    triggers: ["go/**"]
    local:
      command: ""
      test_command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: ""
      job: ""
    requirements: []
    ci_only_reason: ""
    local_only_reason: "permanently local-only by design"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error for a gate with only test_command set: %v", err)
	}
	if reg.Gates[0].Local == nil || reg.Gates[0].Local.Command != "" || reg.Gates[0].Local.TestCommand == "" {
		t.Errorf("Local = %+v, want Command empty and TestCommand set", reg.Gates[0].Local)
	}
}
