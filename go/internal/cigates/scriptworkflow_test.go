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

// TestDriftCheck_VerifyScriptSubstringNotMatched pins the boundary rule in
// workflowRunsScript.
//
// A workflow mentioning "myscripts/verify-no-ai-attribution.sh" contains
// "scripts/verify-no-ai-attribution.sh" as a plain substring while running a
// completely different file. Under strings.Contains that workflow is counted as
// a second host, the script then looks like it has two owners, the
// "exactly one workflow" precondition fails, and the gate is skipped -- so a
// real mismatch silently becomes a pass. That is the failure this check exists
// to prevent, arriving through the check itself.
//
// Verified by mutation: replacing workflowRunsScript's body with
// strings.Contains makes this test fail.
func TestDriftCheck_VerifyScriptSubstringNotMatched(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "harness.yml", "name: H\non: [push]\njobs:\n"+
		"  h:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: myscripts/verify-no-ai-attribution.sh\n")
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
		t.Errorf("a path merely containing the script name must not count as a host; expected the mismatch to be reported")
	}
}

// TestDriftCheck_VerifyScriptPathFilterMentionIsNotAHost pins that only an
// executable invocation counts as a host.
//
// A workflow that merely watches the script in a dorny paths filter is not
// running it. Counting that mention as a host gives the script two apparent
// owners, the "exactly one workflow" precondition fails, and the gate is
// silently skipped -- so a cross-wired gate passes unchecked, which is the
// failure this check exists to prevent arriving through the check itself.
//
// Live instance this reproduces: scripts/verify-golden-corpus-gate.sh is
// watched by static-contract-gates.yml's maturitydrift filter and executed only
// by golden-corpus-gate.yml, so golden-corpus-gate went unvalidated (#5748
// review).
func TestDriftCheck_VerifyScriptPathFilterMentionIsNotAHost(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	// Watches the script in a filter, never runs it.
	writeWorkflow(t, root, "watcher.yml", "name: W\non: [push]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - uses: dorny/paths-filter@v3\n        with:\n          filters: |\n"+
		"            k:\n              - 'scripts/verify-no-ai-attribution.sh'\n")
	writeWorkflow(t, root, "verify-agent-hygiene.yml", runsAttribution)

	// The gate declares the watcher, which does not run it. With the mention
	// counted as a host there are two owners and the gate is skipped silently;
	// only executable-position matching reports the mismatch.
	g := scriptGate("watcher.yml", "bash scripts/verify-no-ai-attribution.sh")
	g.CI.Job = "changes"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "no-ai-attribution", "verify-agent-hygiene.yml") {
			found = true
		}
	}
	if !found {
		t.Errorf("a paths-filter mention must not count as a host; expected the mismatch to be reported")
	}
}
