// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Load's local.pre_push / local.pre_push_reason rules: a gate may defer
// itself out of the `ci-gates run --pre-push` selection lane (the fast local
// floor `scripts/dev/pre-push.sh` runs before every push) only when CI still
// enforces it regardless -- either it is blocking:true with a real CI
// workflow/job (required-gates.yml aggregates every blocking gate
// automatically), or it is advisory (blocking:false), which is never part of
// the required merge gate in the first place. Deferring a gate must move its
// enforcement to CI, never remove it.

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestLoad_PrePushDeferredRequiresReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: slow-gate
    name: Slow Gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-slow.sh"
      pre_push: deferred
    ci:
      workflow: test.yml
      job: "test"
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for pre_push: deferred without pre_push_reason, got nil")
	}
	if !strings.Contains(err.Error(), "pre_push_reason") {
		t.Errorf("error %q should name pre_push_reason", err.Error())
	}
}

func TestLoad_PrePushInvalidValueRejected(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: slow-gate
    name: Slow Gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-slow.sh"
      pre_push: sometimes
      pre_push_reason: "median 70s"
    ci:
      workflow: test.yml
      job: "test"
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for an unrecognized pre_push value, got nil")
	}
	if !strings.Contains(err.Error(), `"sometimes"`) {
		t.Errorf("error %q should name the invalid value", err.Error())
	}
}

func TestLoad_PrePushDeferredBlockingWithoutCIRejected(t *testing.T) {
	t.Parallel()
	// blocking:true with NO ci.workflow/job is the shape this rule exists to
	// reject: deferring it from --pre-push would leave it enforced nowhere.
	yaml := `version: v1
gates:
  - id: slow-gate
    name: Slow Gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-slow.sh"
      pre_push: deferred
      pre_push_reason: "median 70s of the 27-minute pre-push run"
    ci_only_reason: ""
    local_only_reason: "placeholder so the no-CI-backstop rule does not also fire"
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error deferring a blocking gate with no CI workflow/job, got nil")
	}
	if !strings.Contains(err.Error(), "pre_push: deferred") {
		t.Errorf("error %q should name the pre_push: deferred rule", err.Error())
	}
}

func TestLoad_PrePushDeferredBlockingWithCIAccepted(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: slow-gate
    name: Slow Gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-slow.sh"
      test_command: "bash scripts/test-verify-slow.sh"
      pre_push: deferred
      pre_push_reason: "median 70s of the 27-minute pre-push run; still runs in make pre-pr and CI"
    ci:
      workflow: static-contract-gates.yml
      job: "Verify slow gate"
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	gate := reg.Gates[0]
	if gate.Local == nil {
		t.Fatal("gate.Local is nil, want a populated Local block")
	}
	if !gate.Local.PrePushDeferred {
		t.Error("gate.Local.PrePushDeferred = false, want true")
	}
	if gate.Local.PrePushReason == "" {
		t.Error("gate.Local.PrePushReason is empty, want the trimmed reason text")
	}
}

func TestLoad_PrePushDeferredAdvisoryAcceptedRegardlessOfCI(t *testing.T) {
	t.Parallel()
	// blocking:false (advisory) is never part of the required merge gate, so
	// deferring it from --pre-push removes nothing required-gates.yml enforces
	// -- regardless of whether it happens to carry its own separate,
	// non-required CI workflow (code-coverage-report.yml's real shape).
	yaml := `version: v1
gates:
  - id: advisory-gate
    name: Advisory Gate
    category: exactness
    tier: pre-pr
    blocking: false
    triggers: ["go/**"]
    local:
      command: "bash scripts/generate-advisory-report.sh"
      pre_push: deferred
      pre_push_reason: "advisory only; never required for merge"
    ci:
      workflow: advisory-report.yml
      job: "Generate advisory report"
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !reg.Gates[0].Local.PrePushDeferred {
		t.Error("gate.Local.PrePushDeferred = false, want true for an advisory gate")
	}
}

func TestLoad_NoPrePushFieldLeavesDeferredFalse(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, minimalValidYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	gate := reg.Gates[0]
	if gate.Local.PrePushDeferred {
		t.Error("gate.Local.PrePushDeferred = true, want false when pre_push is absent")
	}
	if gate.Local.PrePushReason != "" {
		t.Errorf("gate.Local.PrePushReason = %q, want empty when pre_push is absent", gate.Local.PrePushReason)
	}
}

func TestLoad_PrePushFloorParsed(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: fast-gate
    name: Fast Gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-fast.sh"
      pre_push: floor
    ci:
      workflow: test.yml
      job: "test"
    ci_only_reason: ""
`
	reg, err := cigates.Load(writeYAML(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for pre_push: floor", err)
	}
	local := reg.Gates[0].Local
	if local == nil || !local.PrePushFloor || local.PrePushDeferred {
		t.Fatalf("Local = %+v, want PrePushFloor=true and PrePushDeferred=false", local)
	}
}
