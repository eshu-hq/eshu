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

func TestPlanPRWorkflowsMarkdownNegation(t *testing.T) {
	t.Parallel()
	defs := []PRWorkflowDefinition{
		{File: "e2e-tests.yml", Name: "End-to-end Tests", Synchronize: true, Paths: []string{"go/**", "!**/*.md"}, Jobs: []string{"test"}},
	}
	got, err := PlanPRWorkflows(defs, []string{"go/cmd/collector-cicd-run/README.md"}, false, "main", "full", nil)
	if err != nil {
		// A docs-only PR that selects nothing is a valid empty plan only in
		// reuse mode; in full mode the planner errors, which still proves the
		// heavy workflow was not selected for nested markdown.
		if len(got) != 0 {
			t.Fatalf("nested markdown selected heavy jobs: %#v", got)
		}
		return
	}
	if len(got) != 0 {
		t.Fatalf("nested markdown selected heavy jobs: %#v", got)
	}
	got, err = PlanPRWorkflows(defs, []string{"go/cmd/collector-cicd-run/main.go"}, false, "main", "full", nil)
	if err != nil || !slices.Equal(got["e2e-tests.yml"], []string{"test"}) {
		t.Fatalf("go source must still select the heavy workflow: %#v, %v", got, err)
	}
}

// TestDocsOnlyAggregateIsPlannable proves the docs-only carve-out never
// strands required-gates-complete: every aggregate-selected blocking gate
// must be producible on the PR — its workflow planned with the gate in the
// plan set, and, for test.yml lanes, its job actually running (not
// code-gated). A vector that selects a gate no job produces would wait out
// the await timeout instead of merging.
func TestDocsOnlyAggregateIsPlannable(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Mirror trustedPRWorkflowDefinitions: direct pull_request workflows plus
	// validated callable fragments, so the plan under test is the production
	// plan rather than a fragment-only subset.
	workflowPaths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var defs []PRWorkflowDefinition
	seen := make(map[string]bool)
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
	}
	vectors := [][]string{
		{"docs/public/reference/local-testing.md"},
		{"go/internal/parser/rust/README.md"},
		{"sdk/go/factschema/azure/v1/README.md"},
		{"docs/public/reference/local-testing.md", "package.json"},
		{"skill-fragments/operating-standard.md"},
		{"expected/gemini-cli/GEMINI.md"},
		{"docs/mkdocs.yml"},
		{"docs/public/reference/local-testing.md", "go/cmd/ci-gates/main.go"},
	}
	for _, paths := range vectors {
		required, err := reg.RequiredGates(paths)
		if err != nil {
			t.Fatalf("RequiredGates(%v): %v", paths, err)
		}
		plan, err := PlanPRWorkflows(defs, paths, false, "main", "full", nil)
		if err != nil {
			t.Fatalf("PlanPRWorkflows(%v): %v", paths, err)
		}
		planned, err := SelectPRGateIDs(reg, defs, paths, false, "full", plan)
		if err != nil {
			t.Fatalf("SelectPRGateIDs(%v): %v", paths, err)
		}
		inPlan := make(map[string]struct{}, len(planned))
		for _, id := range planned {
			inPlan[id] = struct{}{}
		}
		for _, gate := range required {
			for _, id := range gate.GateIDs {
				if _, ok := inPlan[id]; !ok {
					t.Errorf("paths %v: aggregate gate %q (job %s/%s) has no producing plan entry — strands the await",
						paths, id, gate.Workflow, gate.Job)
				}
			}
			if gate.Workflow == "test.yml" {
				if allDocsSetChanged(paths) {
					if _, gated := codeGatedCIJobs[gate.Job]; gated {
						t.Errorf("paths %v: aggregate job test.yml/%s never runs on a docs-only PR",
							paths, gate.Job)
					}
				}
				if !slices.Contains(plan["test.yml"], gate.Job) {
					t.Errorf("paths %v: aggregate job test.yml/%s is not in the test.yml plan — never produced",
						paths, gate.Job)
				}
			} else if _, ok := plan[gate.Workflow]; !ok {
				t.Errorf("paths %v: aggregate workflow %s is not planned — call never runs",
					paths, gate.Workflow)
			}
		}
	}
}

func TestDocsOnlyPathsSkipHeavyFragments(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	files := []string{"bench.yml", "build.yml", "e2e-tests.yml", "live-backend-tests.yml", "read-api-latency-gate.yml", "sdk-go.yml"}
	var defs []PRWorkflowDefinition
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, "specs/ci-pr-workflows.d", file))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParsePRPlanFragment(raw)
		if err != nil || len(parsed) != 1 {
			t.Fatalf("fragment %s: definitions=%d error=%v", file, len(parsed), err)
		}
		defs = append(defs, parsed[0])
	}
	nestedMD := []string{"go/cmd/collector-cicd-run/README.md"}
	got, _ := PlanPRWorkflows(defs, nestedMD, false, "main", "full", nil)
	if len(got) != 0 {
		t.Fatalf("nested markdown must not select heavy workflows: %#v", got)
	}
	docsJSON := []string{"docs/public/reference/code-coverage-shield.json"}
	got, _ = PlanPRWorkflows(defs, docsJSON, false, "main", "full", nil)
	if _, ok := got["build.yml"]; ok {
		t.Fatalf("docs JSON must not select the build workflow: %#v", got)
	}
	for _, probe := range [][]string{
		{"package.json"},
		{"package-lock.json"},
		{"scripts/lib/new-tool-inventory.json"},
		{"docs/public/reference/code-coverage-shield.json"},
	} {
		got, _ := PlanPRWorkflows(defs, probe, false, "main", "full", nil)
		for _, heavy := range []string{"bench.yml", "build.yml", "e2e-tests.yml", "live-backend-tests.yml", "read-api-latency-gate.yml", "sdk-go.yml"} {
			if _, ok := got[heavy]; ok {
				t.Fatalf("scoped JSON %v must not select heavy workflow %s: %#v", probe, heavy, got)
			}
		}
	}
	pinned := []string{"specs/ci-images.v1.json"}
	got, err := PlanPRWorkflows(defs, pinned, false, "main", "full", nil)
	if err != nil || slices.Contains(got["e2e-tests.yml"], "test") == false {
		t.Fatalf("an explicitly pinned image JSON must still select e2e: %#v, %v", got, err)
	}
	if _, ok := got["build.yml"]; !ok {
		t.Fatalf("specs image pins stay code: build must remain selected: %#v", got)
	}
	goJSON := []string{"go/cmd/collector-cicd-run/config.json"}
	got, err = PlanPRWorkflows(defs, goJSON, false, "main", "full", nil)
	if err != nil || len(got["e2e-tests.yml"]) == 0 {
		t.Fatalf("go-tree JSON stays code and must still select e2e: %#v, %v", got, err)
	}
	control := []string{"go/cmd/collector-cicd-run/main.go"}
	got, err = PlanPRWorkflows(defs, control, false, "main", "full", nil)
	if err != nil || len(got) == 0 {
		t.Fatalf("go source must still select heavy workflows: %#v, %v", got, err)
	}
}
