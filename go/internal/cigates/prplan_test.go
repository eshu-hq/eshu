// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestPostgresScheduledOwnedPathsSelectAllFullJobs(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	fragment, err := os.ReadFile(filepath.Join(root, "specs/ci-pr-workflows.d/postgres-scheduled-live.yml"))
	if err != nil {
		t.Fatal(err)
	}
	defs, err := ParsePRPlanFragment(fragment)
	if err != nil || len(defs) != 1 {
		t.Fatalf("PostgreSQL fragment: definitions=%d error=%v", len(defs), err)
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github/workflows/postgres-scheduled-live.yml"))
	if err != nil {
		t.Fatal(err)
	}
	def, err := ValidateCallablePRWorkflow(defs[0], workflow)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		".github/actions/postgres-scheduled-bulk/action.yml",
		"specs/ci-pr-workflows.d/postgres-scheduled-live.yml",
	} {
		t.Run(path, func(t *testing.T) {
			jobs, err := PlanPRWorkflows([]PRWorkflowDefinition{def}, []string{path}, false, "main", "full", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(jobs[def.File], def.Jobs) || len(jobs[def.File]) != 7 {
				t.Fatalf("full PostgreSQL jobs = %v; want all seven %v", jobs[def.File], def.Jobs)
			}
		})
	}
}

func TestPlanPRWorkflowsPreservesSynchronizeFilters(t *testing.T) {
	t.Parallel()
	defs := []PRWorkflowDefinition{
		{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core", "go-race-complete"}},
		{File: "frontend.yml", Name: "Frontend", Synchronize: true, Paths: []string{"apps/**"}, Jobs: []string{"console"}},
		{File: "build.yml", Name: "Docker Build", Synchronize: true, PathsIgnore: []string{"docs/**"}, Jobs: []string{"build"}},
		{File: "discord.yml", Name: "Discord", Synchronize: false, Jobs: []string{"invite"}},
	}
	got, err := PlanPRWorkflows(defs, []string{"docs/guide.md"}, false, "main", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"test.yml": {"go-core", "go-race-complete"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("jobs = %#v; want %#v", got, want)
	}
	got, err = PlanPRWorkflows(defs, []string{"apps/console/src/x.ts"}, false, "main", "full", nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("mixed path selection = %#v, %v; want 3 workflows", got, err)
	}
}

func TestPlanPRWorkflowsRejectsEmptyOrUnknownSmoke(t *testing.T) {
	t.Parallel()
	defs := []PRWorkflowDefinition{{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core"}}}
	if _, err := PlanPRWorkflows(defs, []string{"go/x.go"}, false, "main", "smoke", nil); err == nil {
		t.Fatal("smoke without an explicit fast job must not succeed")
	}
	if _, err := PlanPRWorkflows(defs, []string{"go/x.go"}, false, "main", "smoke", map[string][]string{"test.yml": {"unknown"}}); err == nil {
		t.Fatal("unknown fast job must not pass")
	}
	if _, err := PlanPRWorkflows(defs, []string{"go/x.go"}, false, "main", "smoke", map[string][]string{"missing.yml": {"go-core"}}); err == nil {
		t.Fatal("unknown smoke workflow must not be omitted")
	}
	if _, err := PlanPRWorkflows(defs, []string{"go/x.go"}, false, "main", "reuse", nil); err != nil {
		t.Fatalf("a separately proven reuse may select zero jobs: %v", err)
	}
}

func TestParsePRWorkflowDefinitionEventTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		raw      string
		wantSync bool
	}{
		{"default", "name: Default\non:\n  pull_request:\njobs:\n  test:\n    runs-on: ubuntu-latest\n", true},
		{"opened", "name: Opened\non:\n  pull_request:\n    types: [opened]\njobs:\n  invite:\n    runs-on: ubuntu-latest\n", false},
		{"sync", "name: Sync\non:\n  pull_request:\n    types: [opened, synchronize]\njobs:\n  test:\n    runs-on: ubuntu-latest\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, included, err := ParsePRWorkflowDefinition(tc.name+".yml", []byte(tc.raw))
			if err != nil || !included || got.Synchronize != tc.wantSync {
				t.Fatalf("definition = %#v, included=%v, err=%v", got, included, err)
			}
		})
	}
}

func TestPlanPRWorkflowsHonorsBaseBranch(t *testing.T) {
	t.Parallel()
	defs := []PRWorkflowDefinition{
		{File: "main.yml", Name: "Main", Synchronize: true, Branches: []string{"main"}, Jobs: []string{"test"}},
		{File: "release.yml", Name: "Release", Synchronize: true, BranchesIgnore: []string{"main"}, Jobs: []string{"test"}},
	}
	got, err := PlanPRWorkflows(defs, []string{"go/x.go"}, false, "main", "full", nil)
	if err != nil || len(got) != 1 || len(got["main.yml"]) != 1 {
		t.Fatalf("main plan = %#v, err=%v", got, err)
	}
	got, err = PlanPRWorkflows(defs, []string{"go/x.go"}, false, "release/v1", "full", nil)
	if err != nil || len(got) != 1 || len(got["release.yml"]) != 1 {
		t.Fatalf("release plan = %#v, err=%v", got, err)
	}
}

func TestUnknownPRWorkflowDefinitionsForbidTransfer(t *testing.T) {
	t.Parallel()
	reg := &Registry{Gates: []Gate{{CI: CI{Workflow: "test.yml"}}}, NonGateWorkflows: []NonGateWorkflow{{File: "build.yml"}}}
	known := []PRWorkflowDefinition{{File: "test.yml"}, {File: "build.yml"}}
	if HasUnknownPRWorkflowDefinitions(reg, known) {
		t.Fatal("known gate and non-gate workflows were rejected")
	}
	if !HasUnknownPRWorkflowDefinitions(reg, append(known, PRWorkflowDefinition{File: "new.yml"})) {
		t.Fatal("unknown workflow must forbid transfer")
	}
}

func TestSelectPRGateIDsDistinguishesSharedJobCells(t *testing.T) {
	t.Parallel()
	reg := &Registry{Gates: []Gate{
		{ID: "fast", Tier: TierPrePush, Triggers: []string{"go/**"}, CI: CI{Workflow: "static.yml", Job: "verify"}},
		{ID: "heavy", Tier: TierCIHeavy, Triggers: []string{"go/**"}, CI: CI{Workflow: "static.yml", Job: "verify"}},
	}}
	defs := []PRWorkflowDefinition{{File: "static.yml", Name: "Static", Synchronize: true, Jobs: []string{"verify"}}}
	jobs := map[string][]string{"static.yml": {"verify"}}
	full, err := SelectPRGateIDs(reg, defs, []string{"go/a.go"}, false, "full", jobs)
	if err != nil || !reflect.DeepEqual(full, []string{"fast", "heavy"}) {
		t.Fatalf("full gates = %v, err=%v", full, err)
	}
	smoke, err := SelectPRGateIDs(reg, defs, []string{"go/a.go"}, false, "smoke", jobs)
	if err != nil || !reflect.DeepEqual(smoke, []string{"fast"}) {
		t.Fatalf("smoke gates = %v, err=%v", smoke, err)
	}
	reuse, err := SelectPRGateIDs(reg, defs, []string{"go/a.go"}, false, "reuse", map[string][]string{})
	if err != nil || reuse == nil || len(reuse) != 0 {
		t.Fatalf("reuse gates = %v, err=%v", reuse, err)
	}
}

func TestPlanPRWorkflowsSmokeClosesFastNeedsWithoutHeavyAggregateNeeds(t *testing.T) {
	t.Parallel()
	def := PRWorkflowDefinition{
		File: "test.yml", Name: "Build Test", Synchronize: true,
		Jobs: []string{"verify", "test-verify", "queue-selection", "go-core-complete", "go-race-complete", "go-race"},
		JobNeeds: map[string][]string{
			"verify":           {"test-verify"},
			"test-verify":      {"queue-selection"},
			"go-core-complete": {"verify", "go-race"},
			"go-race-complete": {"go-race"},
		},
	}
	fast := map[string][]string{"test.yml": {"verify", "go-core-complete", "go-race-complete"}}
	got, err := PlanPRWorkflows([]PRWorkflowDefinition{def}, []string{"go/x.go"}, false, "main", "smoke", fast)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"verify", "test-verify", "queue-selection", "go-core-complete", "go-race-complete"} {
		if !slices.Contains(got["test.yml"], job) {
			t.Fatalf("smoke lost dependency %s: %v", job, got)
		}
	}
	if slices.Contains(got["test.yml"], "go-race") {
		t.Fatalf("heavy aggregate need re-admitted go-race: %v", got)
	}
}
