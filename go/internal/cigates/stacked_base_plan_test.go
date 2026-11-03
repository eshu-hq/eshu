// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestStackedBasePlansSameGateSetAsMain pins the fix for #9766: a stacked PR
// whose base is a non-default branch must plan the workflow_call-only gate
// workflows exactly as a main-based PR would. Those workflows declare
// `branches: [main]` in their frozen fragments, but a branch filter does not
// apply to a workflow_call, so the planner must ignore it for callable
// workflows rather than dropping every main-gated leaf. Direct pull_request
// workflows keep their branch filter (GitHub itself honors it natively).
func TestStackedBasePlansSameGateSetAsMain(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defs, callable := trustedPRWorkflowDefsForTest(t, root)
	paths := []string{"go/internal/collector/repo/git/fact_builder.go"}

	mainJobs, err := PlanPRWorkflows(defs, paths, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("main plan: %v", err)
	}
	// Guard the fixture: this path must select a callable leaf that declares
	// `branches: [main]`, or the test proves nothing.
	if len(mainJobs["golden-corpus-gate.yml"]) == 0 {
		t.Fatal("test paths no longer select golden-corpus-gate.yml; the fixture does not exercise the branch filter")
	}
	stackedJobs, err := PlanPRWorkflows(defs, paths, false, "drive/9766", "full", nil)
	if err != nil {
		t.Fatalf("stacked plan: %v", err)
	}
	for file := range callable {
		if !reflect.DeepEqual(stackedJobs[file], mainJobs[file]) {
			t.Fatalf("callable %s differs between main and stacked base\n  main:   %v\n  stacked: %v", file, mainJobs[file], stackedJobs[file])
		}
	}

	mainGates, err := SelectPRGateIDs(reg, defs, paths, false, "full", mainJobs)
	if err != nil {
		t.Fatalf("main gates: %v", err)
	}
	stackedGates, err := SelectPRGateIDs(reg, defs, paths, false, "full", stackedJobs)
	if err != nil {
		t.Fatalf("stacked gates: %v", err)
	}
	// A direct workflow whose native pull_request trigger filters on base
	// (delta-union-scheduled.yml targets main) legitimately drops for a
	// stacked base, and its non-blocking gate goes with it. Every other gate
	// must match the main-based plan.
	dropped := make(map[string]bool)
	for _, gate := range reg.Gates {
		if gate.CI.Workflow == "" || callable[gate.CI.Workflow] {
			continue
		}
		if _, ok := stackedJobs[gate.CI.Workflow]; !ok {
			dropped[gate.ID] = true
		}
	}
	want := make([]string, 0, len(mainGates))
	for _, id := range mainGates {
		if !dropped[id] {
			want = append(want, id)
		}
	}
	if !reflect.DeepEqual(stackedGates, want) {
		t.Fatalf("stacked base gate set differs from main gate set\n  main:   %v\n  stacked: %v\n  want:   %v", mainGates, stackedGates, want)
	}
}

// trustedPRWorkflowDefsForTest mirrors trustedPRWorkflowDefinitions: direct
// pull_request workflows plus validated callable fragments, so the plan under
// test is the production plan rather than a subset. The returned set names the
// callable (workflow_call-only) leaves.
func trustedPRWorkflowDefsForTest(t *testing.T, root string) ([]PRWorkflowDefinition, map[string]bool) {
	t.Helper()
	workflowPaths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yamlPaths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowPaths = append(workflowPaths, yamlPaths...)
	sort.Strings(workflowPaths)
	var defs []PRWorkflowDefinition
	seen := make(map[string]bool)
	callable := make(map[string]bool)
	for _, path := range workflowPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		def, include, err := ParsePRWorkflowDefinition(filepath.Base(path), raw)
		if err != nil {
			t.Fatal(err)
		}
		if include {
			defs = append(defs, def)
			seen[def.File] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "specs", "ci-pr-workflows.d"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "specs", "ci-pr-workflows.d", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParsePRPlanFragment(raw)
		if err != nil || len(parsed) != 1 {
			t.Fatalf("fragment %s: definitions=%d error=%v", entry.Name(), len(parsed), err)
		}
		if seen[parsed[0].File] {
			t.Fatalf("duplicate PR workflow %s", parsed[0].File)
		}
		seen[parsed[0].File] = true
		workflowRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", parsed[0].File))
		if err != nil {
			t.Fatal(err)
		}
		def, err := ValidateCallablePRWorkflow(parsed[0], workflowRaw)
		if err != nil {
			t.Fatalf("fragment %s: %v", entry.Name(), err)
		}
		defs = append(defs, def)
		callable[def.File] = true
	}
	return defs, callable
}
