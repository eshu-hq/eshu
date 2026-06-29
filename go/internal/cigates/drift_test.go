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

// buildDriftRepo creates a hermetic repo root for drift tests.
// preCommitYAML is written verbatim to .pre-commit-config.yaml.
// workflows is a list of filenames to create under .github/workflows/.
func buildDriftRepo(t *testing.T, preCommitYAML string, workflows []string) string {
	t.Helper()
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pre-commit-config.yaml"), []byte(preCommitYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, w := range workflows {
		p := filepath.Join(wfDir, w)
		stub := "name: test\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: []\n"
		if err := os.WriteFile(p, []byte(stub), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// minimalPreCommit returns a .pre-commit-config.yaml with a single local hook
// whose id is hookID.
func minimalPreCommit(hookID string) string {
	return `repos:
  - repo: local
    hooks:
      - id: ` + hookID + `
        name: test hook
        entry: scripts/check.sh
        language: script
        pass_filenames: false
`
}

// minimalPreCommitHygiene returns a config with a single local hook that is
// in the hygiene_hooks list.
func minimalPreCommitHygiene() string {
	return `repos:
  - repo: local
    hooks:
      - id: trailing-whitespace
        name: trailing whitespace
        entry: trailing-whitespace
        language: system
`
}

// minimalReg builds a Registry for drift tests.
func minimalReg(gates []cigates.Gate, hygiene []cigates.HygieneHook, nonGateWFs []cigates.NonGateWorkflow) *cigates.Registry {
	return &cigates.Registry{
		Version:          "v1",
		Gates:            gates,
		HygieneHooks:     hygiene,
		NonGateWorkflows: nonGateWFs,
	}
}

// gateWith returns a minimal valid Gate with the given id, optional hook_id, and workflow.
func gateWith(id, hookID, workflow string) cigates.Gate {
	g := cigates.Gate{
		ID:       id,
		Name:     id,
		Category: cigates.CategoryHygiene,
		Tier:     cigates.TierPreCommit,
		Blocking: true,
		Triggers: []string{"go/**"},
		Local: &cigates.Local{
			Command: "bash scripts/check.sh",
		},
		CI: cigates.CI{Workflow: workflow, Job: "job"},
	}
	g.HookID = hookID
	return g
}

// ── Test 1: clean registry + matching hooks + workflows → zero errors ─────────

func TestDriftCheck_Clean(t *testing.T) {
	t.Parallel()

	preCommit := minimalPreCommit("my-gate")
	root := buildDriftRepo(t, preCommit, []string{"verify.yml"})

	reg := minimalReg(
		[]cigates.Gate{gateWith("my-gate", "my-gate", "verify.yml")},
		nil,
		nil,
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) != 0 {
		t.Errorf("expected no errors for clean tree, got %d: %v", len(errs), errs)
	}
}

// ── Test 2: unregistered local hook → error ────────────────────────────────────

func TestDriftCheck_UnregisteredHook(t *testing.T) {
	t.Parallel()

	preCommit := minimalPreCommit("unregistered-hook")
	root := buildDriftRepo(t, preCommit, []string{"verify.yml"})

	reg := minimalReg(
		// The gate has a different hook_id — "unregistered-hook" has no gate and is not hygiene.
		[]cigates.Gate{gateWith("some-other-gate", "other-hook", "verify.yml")},
		nil,
		nil,
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected error for unregistered hook, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "unregistered-hook") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q, got: %v", "unregistered-hook", errs)
	}
}

// ── Test 3: hook in hygiene_hooks → allowed, no error ─────────────────────────

func TestDriftCheck_HygieneHookAllowed(t *testing.T) {
	t.Parallel()

	preCommit := minimalPreCommitHygiene()
	root := buildDriftRepo(t, preCommit, nil)

	reg := minimalReg(
		nil,
		[]cigates.HygieneHook{{ID: "trailing-whitespace", Reason: "whitespace hygiene"}},
		nil,
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) != 0 {
		t.Errorf("hygiene hook should be allowed, got %d errors: %v", len(errs), errs)
	}
}

// ── Test 4: gate hook_id missing from .pre-commit-config → error ──────────────

func TestDriftCheck_GateHookIDMissingFromPreCommit(t *testing.T) {
	t.Parallel()

	// .pre-commit-config has no hooks at all
	preCommit := `repos:
  - repo: local
    hooks: []
`
	root := buildDriftRepo(t, preCommit, []string{"verify.yml"})

	reg := minimalReg(
		[]cigates.Gate{gateWith("my-gate", "my-gate", "verify.yml")},
		nil,
		nil,
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected error for gate hook_id missing from .pre-commit-config, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "my-gate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning gate %q, got: %v", "my-gate", errs)
	}
}

// ── Test 5: hook stage mismatch → error ───────────────────────────────────────

func TestDriftCheck_HookStageMismatch(t *testing.T) {
	t.Parallel()

	// Gate is tier pre-commit but hook declares only pre-push stage
	preCommit := `repos:
  - repo: local
    hooks:
      - id: my-gate
        name: my gate
        entry: scripts/check.sh
        language: script
        stages: [pre-push]
`
	root := buildDriftRepo(t, preCommit, []string{"verify.yml"})

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Tier = cigates.TierPreCommit

	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected stage mismatch error, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "stage") || strings.Contains(e.Error(), "my-gate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning stage mismatch for %q, got: %v", "my-gate", errs)
	}
}

// ── Test 6: workflow in neither gate nor non_gate_workflows → error ───────────

func TestDriftCheck_WorkflowUnregistered(t *testing.T) {
	t.Parallel()

	preCommit := `repos:
  - repo: local
    hooks: []
`
	root := buildDriftRepo(t, preCommit, []string{"orphan.yml"})

	reg := minimalReg(nil, nil, nil) // no gates, no non_gate_workflows

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected error for unregistered workflow, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "orphan.yml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q, got: %v", "orphan.yml", errs)
	}
}

// ── Test 7: workflow in non_gate_workflows → allowed ──────────────────────────

func TestDriftCheck_WorkflowInNonGate(t *testing.T) {
	t.Parallel()

	preCommit := `repos:
  - repo: local
    hooks: []
`
	root := buildDriftRepo(t, preCommit, []string{"deploy-docs.yml"})

	reg := minimalReg(
		nil,
		nil,
		[]cigates.NonGateWorkflow{{File: "deploy-docs.yml", Reason: "docs deploy, not a PR gate"}},
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) != 0 {
		t.Errorf("non_gate_workflow entry should be allowed, got %d errors: %v", len(errs), errs)
	}
}

// ── Test 8: stale non_gate_workflows entry (file not on disk) → error ─────────

func TestDriftCheck_StaleNonGateWorkflow(t *testing.T) {
	t.Parallel()

	preCommit := `repos:
  - repo: local
    hooks: []
`
	// No workflows on disk at all.
	root := buildDriftRepo(t, preCommit, nil)

	reg := minimalReg(
		nil,
		nil,
		[]cigates.NonGateWorkflow{{File: "gone.yml", Reason: "should exist but was deleted"}},
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected error for stale non_gate_workflows entry, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "gone.yml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q, got: %v", "gone.yml", errs)
	}
}

// ── Test 9: workflow referenced by gate AND listed in non_gate_workflows → error

func TestDriftCheck_WorkflowInBothGateAndNonGate(t *testing.T) {
	t.Parallel()

	preCommit := `repos:
  - repo: local
    hooks:
      - id: my-gate
        name: my gate
        entry: scripts/check.sh
        language: script
`
	root := buildDriftRepo(t, preCommit, []string{"verify.yml"})

	reg := minimalReg(
		[]cigates.Gate{gateWith("my-gate", "my-gate", "verify.yml")},
		nil,
		// Also listed in non_gate_workflows — must be an error.
		[]cigates.NonGateWorkflow{{File: "verify.yml", Reason: "should not be here too"}},
	)

	errs := cigates.DriftCheck(root, reg)
	if len(errs) == 0 {
		t.Fatal("expected error for workflow in both gate and non_gate_workflows, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "verify.yml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q, got: %v", "verify.yml", errs)
	}
}
