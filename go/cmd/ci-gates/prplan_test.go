// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestTrustedPRWorkflowDefinitionsKeepNonGateEventSemantics(t *testing.T) {
	defs, err := trustedPRWorkflowDefinitions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	byFile := make(map[string]cigates.PRWorkflowDefinition)
	for _, def := range defs {
		byFile[def.File] = def
	}
	for _, file := range []string{"test.yml", "build.yml", "post_discord_invite.yml", "refresh-cassettes.yml"} {
		if _, ok := byFile[file]; !ok {
			t.Fatalf("missing PR workflow %s", file)
		}
	}
	if !byFile["test.yml"].Synchronize || !byFile["build.yml"].Synchronize {
		t.Fatal("synchronize workflows were not parsed")
	}
	if byFile["post_discord_invite.yml"].Synchronize || byFile["refresh-cassettes.yml"].Synchronize {
		t.Fatal("opened/labeled-only workflows must not join synchronize plan")
	}
	if !reflect.DeepEqual(byFile["build.yml"].Branches, []string{"main"}) {
		t.Fatalf("build workflow lost base branch filter: %v", byFile["build.yml"].Branches)
	}
	if len(byFile["build.yml"].PathsIgnore) == 0 {
		t.Fatal("non-gate build workflow lost paths-ignore")
	}
}

func TestBuildPRPlanWorkflowDefinitionChangeForcesFull(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"--json headRefOid":   headSHAFixture,
		"--json changedFiles": "1",
		"--json baseRefName":  "main",
		"/pulls/42/files":     `[[{"filename":".github/workflows/test.yml"}]]`,
	}}
	defs := []cigates.PRWorkflowDefinition{
		{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core", "go-race-complete"}},
		{File: "opened.yml", Name: "Opened", Synchronize: false, Jobs: []string{"invite"}},
	}
	reg := &cigates.Registry{Gates: []cigates.Gate{{ID: "go-core", Tier: cigates.TierPrePR, Triggers: []string{".github/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "go-core"}}}}
	got, err := buildPRPlan(context.Background(), runner, reg, defs, "eshu-hq/eshu", 42, headSHAFixture)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Mode != "full" || got.HeadSHA != headSHAFixture || !reflect.DeepEqual(got.Workflows, []string{"test.yml"}) || !reflect.DeepEqual(got.Jobs["test.yml"], []string{"go-core", "go-race-complete"}) || !reflect.DeepEqual(got.Gates, []string{"go-core"}) {
		t.Fatalf("plan = %#v", got)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "git/commits") || strings.Contains(call, "graphql") {
			t.Fatalf("definition change tried a transfer probe: %s", call)
		}
	}
}

func TestValidatePRPlanRejectsMissingAndUnsupportedOutputs(t *testing.T) {
	t.Parallel()
	good := prPlan{SchemaVersion: 1, Mode: "full", HeadSHA: headSHAFixture, Workflows: []string{"test.yml"}, Jobs: map[string][]string{"test.yml": {"go-core"}}, Gates: []string{"go-core"}, Reason: "full"}
	if err := validatePRPlan(good); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	bad := []prPlan{
		{SchemaVersion: 2, Mode: good.Mode, HeadSHA: good.HeadSHA, Workflows: good.Workflows, Jobs: good.Jobs, Gates: good.Gates, Reason: good.Reason},
		{SchemaVersion: 1, Mode: "full", HeadSHA: headSHAFixture, Workflows: []string{}, Jobs: map[string][]string{}, Gates: []string{}, Reason: "full"},
		{SchemaVersion: 1, Mode: "reuse", HeadSHA: headSHAFixture, Workflows: []string{}, Jobs: map[string][]string{}, Gates: []string{}, Reason: "reuse"},
		{SchemaVersion: 1, Mode: "smoke", HeadSHA: headSHAFixture, Workflows: []string{"test.yml"}, Jobs: map[string][]string{}, Gates: []string{}, Reason: "smoke"},
		{SchemaVersion: 1, Mode: "smoke", HeadSHA: headSHAFixture, Workflows: good.Workflows, Jobs: good.Jobs, Gates: good.Gates, Reason: "smoke"},
		{SchemaVersion: 1, Mode: "full", HeadSHA: headSHAFixture, SourceSHA: reuseOldSHA, Tree: reuseOtherTree, Workflows: good.Workflows, Jobs: good.Jobs, Gates: good.Gates, Reason: "full"},
	}
	for _, plan := range bad {
		if err := validatePRPlan(plan); err == nil {
			t.Fatalf("invalid plan accepted: %#v", plan)
		}
	}
}

func TestTrustedPRWorkflowDefinitionsIncludeCallableFragment(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	fragmentDir := filepath.Join(root, "specs", "ci-pr-workflows.d")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := "name: Frontend\non:\n  workflow_call:\njobs:\n  console:\n    name: Console\n    runs-on: ubuntu-latest\n"
	if err := os.WriteFile(filepath.Join(workflowDir, "frontend.yml"), []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := "schema_version: 1\nworkflows:\n  - file: frontend.yml\n    name: Frontend\n    synchronize: true\n    branches: [main]\n    paths: ['apps/**']\n    jobs: [console]\n    job_names: {console: Console}\n"
	if err := os.WriteFile(filepath.Join(fragmentDir, "frontend.yaml"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}
	defs, err := trustedPRWorkflowDefinitions(root)
	if err != nil || len(defs) != 1 || defs[0].File != "frontend.yml" || !reflect.DeepEqual(defs[0].Paths, []string{"apps/**"}) {
		t.Fatalf("callable fragment = %#v, err=%v", defs, err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "frontend.yml"), []byte(strings.Replace(workflow, "workflow_call:", "pull_request:\n  workflow_call:", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trustedPRWorkflowDefinitions(root); err == nil {
		t.Fatal("manifest leaf retained a direct pull_request trigger")
	}
}

func TestTrustedPRWorkflowDefinitionsRejectManifestDrift(t *testing.T) {
	for _, fragment := range []string{
		"schema_version: 2\nworkflows:\n  - file: frontend.yml\n    name: Frontend\n    synchronize: true\n    jobs: [console]\n    job_names: {console: Console}\n",
		"schema_version: 1\nworkflows:\n  - file: frontend.yml\n    name: Frontend\n    synchronize: true\n    jobs: [missing]\n    job_names: {missing: Console}\n",
	} {
		root := t.TempDir()
		workflowDir := filepath.Join(root, ".github", "workflows")
		fragmentDir := filepath.Join(root, "specs", "ci-pr-workflows.d")
		if err := os.MkdirAll(workflowDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workflowDir, "frontend.yml"), []byte("name: Frontend\non: {workflow_call: {}}\njobs:\n  console:\n    name: Console\n    runs-on: ubuntu-latest\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fragmentDir, "frontend.yaml"), []byte(fragment), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := trustedPRWorkflowDefinitions(root); err == nil {
			t.Fatalf("drift accepted: %s", fragment)
		}
	}
}

func TestSmokeJobIDsKeepRequiredGoUmbrellas(t *testing.T) {
	t.Parallel()
	reg := &cigates.Registry{
		Gates:                []cigates.Gate{{ID: "fast", Blocking: true, Tier: cigates.TierPrePush, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "go-core"}}},
		RequiredStatusChecks: []cigates.RequiredStatusCheck{{Context: "go-core-complete", Workflow: "test.yml", Job: "go-core-complete"}, {Context: "go-race-complete", Workflow: "test.yml", Job: "go-race-complete"}},
	}
	defs := []cigates.PRWorkflowDefinition{{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core", "go-core-complete", "go-race-complete"}}}
	got, err := smokeJobIDs(reg, defs, []string{"go/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"go-core", "go-core-complete", "go-race-complete"} {
		if !slices.Contains(got["test.yml"], job) {
			t.Fatalf("smoke lost %s: %v", job, got)
		}
	}
}

func TestBuildPRPlanRejectsMissingSelectedGateWorkflow(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"--json headRefOid":   headSHAFixture,
		"--json baseRefName":  "main",
		"--json changedFiles": "1",
		"/pulls/42/files":     `[[{"filename":".github/workflows/test.yml"}]]`,
	}}
	defs := []cigates.PRWorkflowDefinition{{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core"}}}
	reg := &cigates.Registry{Gates: []cigates.Gate{{ID: "missing", Blocking: true, Triggers: []string{".github/**"}, CI: cigates.CI{Workflow: "missing.yml", Job: "verify"}}}}
	if _, err := buildPRPlan(context.Background(), runner, reg, defs, "eshu-hq/eshu", 42, headSHAFixture); err == nil {
		t.Fatal("selected blocking workflow absent from direct or fragment inventory")
	}
}

func TestSmokeJobIDsRejectsAggregateOnlySelection(t *testing.T) {
	t.Parallel()
	reg := &cigates.Registry{
		Gates:                []cigates.Gate{{ID: "heavy", Blocking: true, Tier: cigates.TierCIHeavy, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "go-race"}}},
		RequiredStatusChecks: []cigates.RequiredStatusCheck{{Context: "go-core-complete", Workflow: "test.yml", Job: "go-core-complete"}, {Context: "go-race-complete", Workflow: "test.yml", Job: "go-race-complete"}},
	}
	defs := []cigates.PRWorkflowDefinition{{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-race", "go-core-complete", "go-race-complete"}}}
	if _, err := smokeJobIDs(reg, defs, []string{"go/x.go"}); err == nil {
		t.Fatal("aggregate-only smoke must fall back to full")
	}
}
