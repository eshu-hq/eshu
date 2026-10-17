// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// Slim lane contract (#7859 P3): merge_group selection serves the registry's
// fixed slim set, never the changed-path set. Pull-request selection is
// unchanged (pinned by TestPathSelectionUnchangedBySlimLane in cigates).

const slimCmdRegistryYAML = `version: v1
gates:
  - id: slim
    name: Slim
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/slim.sh"
    local:
      command: "bash scripts/slim.sh"
    ci:
      workflow: test.yml
      job: "merge-group-slim"
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
merge_group_slim_gates:
  - slim
`

func slimCmdRegistry(t *testing.T) (*cigates.Registry, *cigates.Registry) {
	t.Helper()
	load := func() *cigates.Registry {
		path := filepath.Join(t.TempDir(), "ci-gates.v1.yaml")
		if err := os.WriteFile(path, []byte(slimCmdRegistryYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		reg, err := cigates.Load(path)
		if err != nil {
			t.Fatalf("Load slim registry: %v", err)
		}
		return reg
	}
	return load(), load()
}

func TestQueueSelectServesSlimSetWithoutChangedPaths(t *testing.T) {
	t.Parallel()
	head, trusted := slimCmdRegistry(t)
	// No routes: any `gh api` call fails. The slim lane must not read changed
	// paths at all, so selection succeeds with zero API calls.
	runner := &endpointRunner{routes: map[string]string{}}
	got, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{head, trusted}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"slim"}) || !reflect.DeepEqual(got.Jobs, []string{"merge-group-slim"}) {
		t.Fatalf("selection = %+v; want slim/merge-group-slim", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("slim selection must not call gh; calls = %v", runner.calls)
	}
}

func TestQueueSelectSlimFailsClosedOnBadBranch(t *testing.T) {
	t.Parallel()
	head, trusted := slimCmdRegistry(t)
	runner := &endpointRunner{routes: map[string]string{}}
	if _, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{head, trusted}, "eshu-hq/eshu", "main", "feature/pr-1", headSHAFixture, "test.yml", ""); err == nil {
		t.Fatal("expected fail-closed error for a non-queue branch")
	}
}

func writeWorkflowStub(t *testing.T, root, file, name string) {
	t.Helper()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "name: " + name + "\non:\n  merge_group:\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAwaitMergeGroupWaitsOnlyOnSlimGates(t *testing.T) {
	t.Parallel()
	head, _ := slimCmdRegistry(t)
	root := t.TempDir()
	writeWorkflowStub(t, root, "test.yml", "Build Test")
	// The path-selected job reports failure; the slim job reports success.
	// Old path selection would fail the await; the slim lane must pass.
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[{"id":7,"name":"Build Test","event":"merge_group","check_suite_id":70}]}]`,
		"check-runs?": `[{"check_runs":[
			{"name":"merge-group-slim","status":"completed","conclusion":"success","check_suite":{"id":70}},
			{"name":"go-core","status":"completed","conclusion":"failure","check_suite":{"id":70}}
		]}]`,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runAwaitMergeGroup(ctx, runner, head, root, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, time.Millisecond); err != nil {
		t.Fatalf("slim await must ignore the failing non-slim job: %v", err)
	}
}

func TestAwaitMergeGroupFailsWhenSlimGateIsRed(t *testing.T) {
	t.Parallel()
	head, _ := slimCmdRegistry(t)
	root := t.TempDir()
	writeWorkflowStub(t, root, "test.yml", "Build Test")
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[{"id":7,"name":"Build Test","event":"merge_group","check_suite_id":70}]}]`,
		"check-runs?": `[{"check_runs":[
			{"name":"merge-group-slim","status":"completed","conclusion":"failure","check_suite":{"id":70}},
			{"name":"go-core","status":"completed","conclusion":"success","check_suite":{"id":70}}
		]}]`,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runAwaitMergeGroup(ctx, runner, head, root, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, time.Millisecond); err == nil {
		t.Fatal("slim await must fail when the slim gate is red")
	}
}
