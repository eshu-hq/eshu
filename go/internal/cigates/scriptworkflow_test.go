// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func scriptGate(workflow, command string) cigates.Gate {
	g := gateWith("no-ai-attribution", "my-gate", workflow)
	g.Local = &cigates.Local{Command: command}
	return g
}

const runsAttribution = "name: Agent hygiene\non: [push]\njobs:\n" +
	"  verify:\n    name: Agent hygiene gate\n    runs-on: ubuntu-latest\n    steps:\n" +
	"      - name: No AI attribution\n        run: scripts/verify-no-ai-attribution.sh\n"

const runsNothingRelevant = "name: Test\non: [push]\njobs:\n" +
	"  docs-helm-hygiene:\n    runs-on: ubuntu-latest\n    steps:\n" +
	"      - name: Lint Helm chart\n        run: helm lint deploy/helm/eshu\n"

// TestDriftCheck_VerifyScriptWorkflowMismatch reproduces #5748: the gate
// declares a workflow whose job exists but never runs its check.
// checkJobNamesResolve proves membership only, so this passed drift validation.
func TestDriftCheck_VerifyScriptWorkflowMismatch(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", runsNothingRelevant)
	writeWorkflow(t, root, "verify-agent-hygiene.yml", runsAttribution)

	g := scriptGate("test.yml", "bash scripts/verify-no-ai-attribution.sh")
	g.CI.Job = "docs-helm-hygiene"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "no-ai-attribution", "verify-agent-hygiene.yml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected drift naming the workflow that actually runs the script")
	}
}

// TestDriftCheck_VerifyScriptWorkflowMatch is the GREEN counterpart.
func TestDriftCheck_VerifyScriptWorkflowMatch(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	// Only the workflow that runs the script: an extra unowned workflow would
	// trip checkWorkflowCompleteness and mask what this case asserts.
	writeWorkflow(t, root, "verify-agent-hygiene.yml", runsAttribution)

	g := scriptGate("verify-agent-hygiene.yml", "bash scripts/verify-no-ai-attribution.sh")
	g.CI.Job = "Agent hygiene gate"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("gate declaring the workflow that runs its script should not drift, got: %v", errs)
	}
}

// TestDriftCheck_VerifyScriptRunByNoWorkflowSkipped: CI legitimately uses a
// different entrypoint for many gates (golangci-lint directly rather than the
// local precommit wrapper; generate-and-diff rather than the verify script). A
// script no workflow invokes carries no correspondence signal and must not be
// reported, or this check would flag ~15 correctly-wired gates.
func TestDriftCheck_VerifyScriptRunByNoWorkflowSkipped(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", runsNothingRelevant)

	g := scriptGate("test.yml", "bash scripts/verify-docs-build-changed.sh")
	g.CI.Job = "docs-helm-hygiene"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("a script no workflow runs carries no signal and must be skipped, got: %v", errs)
	}
}

// TestDriftCheck_VerifyScriptSubstringNotMatched pins the boundary rule.
// Searching for "scripts/verify-fact-kind-registry.sh" as a plain substring
// also matches "scripts/test-verify-fact-kind-registry.sh", which would make a
// workflow that only runs the self-test harness look like it runs the gate --
// turning a real mismatch into a false pass (#5748).
func TestDriftCheck_VerifyScriptSubstringNotMatched(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", runsNothingRelevant)
	writeWorkflow(t, root, "harness.yml", "name: H\non: [push]\njobs:\n"+
		"  h:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: scripts/test-verify-no-ai-attribution.sh\n")
	writeWorkflow(t, root, "verify-agent-hygiene.yml", runsAttribution)

	g := scriptGate("harness.yml", "bash scripts/verify-no-ai-attribution.sh")
	g.CI.Job = "h"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "no-ai-attribution", "verify-agent-hygiene.yml") {
			found = true
		}
	}
	if !found {
		t.Errorf("test-verify-*.sh must not satisfy a search for verify-*.sh; expected the mismatch to be reported")
	}
}
