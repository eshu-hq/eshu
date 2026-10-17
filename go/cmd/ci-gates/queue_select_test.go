// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// Merge-group queue selection serves the registry's fixed slim set (#7859
// P3), never the changed-path set: the same slim jobs are selected for every
// path payload, and no compare call is made.

// slimQueueRegistryYAML is loaded through cigates.Load so the slim-set
// validation the production registry passes also guards these fixtures.
const slimQueueRegistryYAML = `version: v1
gates:
  - id: slim-core
    name: Slim Core
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-slim-core.sh"
    local:
      command: "bash scripts/verify-slim-core.sh"
    ci:
      workflow: test.yml
      job: "merge-group-slim"
  - id: slim-docs
    name: Slim Docs
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-slim-docs.sh"
    local:
      command: "bash scripts/verify-slim-docs.sh"
    ci:
      workflow: test.yml
      job: "docs-slim"
  - id: path-only
    name: Path Only
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "bash scripts/verify-path-only.sh"
    ci:
      workflow: test.yml
      job: "go-core"
  - id: other-workflow
    name: Other Workflow
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "bash scripts/verify-other.sh"
    ci:
      workflow: other.yml
      job: "other"
merge_group_slim_gates:
  - slim-core
  - slim-docs
`

func slimQueueTestRegistry(t *testing.T) *cigates.Registry {
	t.Helper()
	path := t.TempDir() + "/ci-gates.v1.yaml"
	writeTestFile(t, path, slimQueueRegistryYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load slim queue fixture: %v", err)
	}
	return reg
}

func queueRunner(files string) *endpointRunner {
	return &endpointRunner{routes: map[string]string{"compare/": `{"files":` + files + `}`}}
}

func TestQueueSelectServesSlimSetRegardlessOfPaths(t *testing.T) {
	t.Parallel()
	reg := slimQueueTestRegistry(t)
	// No routes: any `gh api` call fails. Every payload below — code,
	// docs, policy edits, and an empty listing — must select the same fixed
	// set with zero API calls.
	runner := &endpointRunner{routes: map[string]string{}}
	for name := range map[string]struct{}{
		"go change":     {},
		"docs change":   {},
		"workflow edit": {},
		"registry edit": {},
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{reg}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"slim-core", "slim-docs"}) {
				t.Fatalf("selection = %+v; want slim-core+slim-docs", got)
			}
			if !reflect.DeepEqual(got.Jobs, []string{"merge-group-slim", "docs-slim"}) {
				t.Fatalf("jobs = %v", got.Jobs)
			}
		})
	}
	if len(runner.calls) != 0 {
		t.Fatalf("fixed-set selection must not call gh; calls = %v", runner.calls)
	}
}

func TestQueueSelectUnionsTrustedRegistryAndMatchesConcreteJob(t *testing.T) {
	t.Parallel()
	head := slimQueueTestRegistry(t)
	trustedYAML := `version: v1
gates:
  - id: trusted-slim
    name: Trusted Slim
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-trusted-slim.sh"
    local:
      command: "bash scripts/verify-trusted-slim.sh"
    ci:
      workflow: test.yml
      job: "merge-group-slim"
      check_names: ["merge-group-slim concrete"]
merge_group_slim_gates:
  - trusted-slim
`
	path := t.TempDir() + "/ci-gates.v1.yaml"
	writeTestFile(t, path, trustedYAML)
	trusted, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load trusted slim fixture: %v", err)
	}
	runner := &endpointRunner{routes: map[string]string{}}
	got, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{head, trusted}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "merge-group-slim concrete")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"trusted-slim"}) || !reflect.DeepEqual(got.Jobs, []string{"merge-group-slim"}) {
		t.Fatalf("selection = %+v; want trusted-slim/merge-group-slim", got)
	}
}

func TestQueueSelectFailsClosedForUnknownWorkflowAndBadBranch(t *testing.T) {
	t.Parallel()
	reg := slimQueueTestRegistry(t)
	runner := &endpointRunner{routes: map[string]string{}}
	for _, tt := range []struct {
		name     string
		workflow string
		branch   string
	}{
		{name: "unknown workflow", workflow: "unknown.yml", branch: queueBranchFixture},
		{name: "bad branch", workflow: "test.yml", branch: "gh-readonly-queue/main/pr-1"},
		{name: "non-queue branch", workflow: "test.yml", branch: "feature/x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{reg}, "eshu-hq/eshu", "main", tt.branch, headSHAFixture, tt.workflow, "")
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}

func TestQueueSelectUnselectedKnownWorkflow(t *testing.T) {
	t.Parallel()
	got, err := selectQueueGates(context.Background(), queueRunner(`[{"filename":"unrelated/x.txt"}]`), []*cigates.Registry{slimQueueTestRegistry(t)}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "other.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected || len(got.Gates) != 0 || len(got.Jobs) != 0 {
		t.Fatalf("selection = %+v", got)
	}
}
