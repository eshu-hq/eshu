// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func writeSlimFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// Slim lane contract (#7859 P3): on merge_group the selection layer serves an
// event-scoped fixed set of blocking gates declared by the registry, ignoring
// changed paths. Pull-request (path) selection is unchanged.

const slimFixtureYAML = `version: v1
gates:
  - id: slim-a
    name: Slim A
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-slim-a.sh"
    local:
      command: "bash scripts/verify-slim-a.sh"
    ci:
      workflow: test.yml
      job: "merge-group-slim"
  - id: slim-b
    name: Slim B
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-slim-b.sh"
    local:
      command: "bash scripts/verify-slim-b.sh"
    ci:
      workflow: static-contract-gates.yml
      job: "Verify slim-b gate"
  - id: path-gate
    name: Path Gate
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "bash scripts/verify-path-gate.sh"
    ci:
      workflow: test.yml
      job: "go-core"
merge_group_slim_gates:
  - slim-a
  - slim-b
`

func loadSlimFixture(t *testing.T) *Registry {
	t.Helper()
	return mustLoadSlimFixture(t, slimFixtureYAML)
}

func mustLoadSlimFixture(t *testing.T, content string) *Registry {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/ci-gates.v1.yaml"
	if err := writeSlimFile(path, content); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load slim fixture: %v", err)
	}
	return reg
}

func TestMergeGroupSlimSetLoadsFromRegistry(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	got := reg.MergeGroupSlim()
	if !reflect.DeepEqual(got, []string{"slim-a", "slim-b"}) {
		t.Fatalf("MergeGroupSlim = %v; want [slim-a slim-b]", got)
	}
}

func TestMergeGroupSlimRejectsUnknownGateID(t *testing.T) {
	t.Parallel()
	bad := strings.Replace(slimFixtureYAML, "  - slim-b\n", "  - slim-b\n  - no-such-gate\n", 1)
	dir := t.TempDir()
	path := dir + "/ci-gates.v1.yaml"
	if err := writeSlimFile(path, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load with unknown slim gate id: expected error")
	} else if !strings.Contains(err.Error(), "no-such-gate") {
		t.Fatalf("Load error %v does not name the unknown slim gate", err)
	}
}

func TestMergeGroupSlimRejectsNonBlockingGate(t *testing.T) {
	t.Parallel()
	advisory := strings.Replace(slimFixtureYAML, "    blocking: true\n    triggers:\n      - \"scripts/verify-slim-b.sh\"", "    blocking: false\n    triggers:\n      - \"scripts/verify-slim-b.sh\"", 1)
	dir := t.TempDir()
	path := dir + "/ci-gates.v1.yaml"
	if err := writeSlimFile(path, advisory); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load with non-blocking slim gate: expected error")
	} else if !strings.Contains(err.Error(), "slim-b") {
		t.Fatalf("Load error %v does not name the non-blocking slim gate", err)
	}
}

func TestMergeGroupSlimRejectsDuplicateGateID(t *testing.T) {
	t.Parallel()
	dup := strings.Replace(slimFixtureYAML, "  - slim-b\n", "  - slim-b\n  - slim-a\n", 1)
	dir := t.TempDir()
	path := dir + "/ci-gates.v1.yaml"
	if err := writeSlimFile(path, dup); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load with duplicated slim gate id: expected error")
	}
}

func TestSelectSlimWorkflowServesFixedSetRegardlessOfPaths(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	for _, paths := range [][]string{
		{"go/internal/graph/a.go"},
		{"docs/anything.md"},
		{"unrelated/x.txt"},
		{},
	} {
		got, err := SelectSlimWorkflow([]*Registry{reg}, "test.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"slim-a"}) || !reflect.DeepEqual(got.Jobs, []string{"merge-group-slim"}) {
			t.Fatalf("paths %v: selection = %+v; want slim-a/merge-group-slim", paths, got)
		}
	}
}

func TestSelectSlimWorkflowUnionsTrustedRegistry(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	trustedOnly := `version: v1
gates:
  - id: legacy-slim
    name: Legacy Slim
    category: hygiene
    tier: pre-push
    blocking: true
    triggers:
      - "scripts/verify-legacy-slim.sh"
    local:
      command: "bash scripts/verify-legacy-slim.sh"
    ci:
      workflow: test.yml
      job: "merge-group-slim"
merge_group_slim_gates:
  - legacy-slim
`
	trusted := mustLoadSlimFixture(t, trustedOnly)
	got, err := SelectSlimWorkflow([]*Registry{reg, trusted}, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"slim-a", "legacy-slim"}) {
		t.Fatalf("union selection = %+v; want [slim-a legacy-slim]", got)
	}
	if !reflect.DeepEqual(got.Jobs, []string{"merge-group-slim"}) {
		t.Fatalf("union jobs = %v; want [merge-group-slim]", got.Jobs)
	}
}

func TestSelectSlimWorkflowFailsClosedOnUnknownOwner(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	for _, tt := range []struct{ workflow, job string }{{"missing.yml", ""}, {"test.yml", "missing"}} {
		if _, err := SelectSlimWorkflow([]*Registry{reg}, tt.workflow, tt.job); err == nil {
			t.Fatalf("workflow %q job %q: expected error", tt.workflow, tt.job)
		}
	}
}

func TestSlimGatesResolveToRequiredJobs(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	required, err := reg.SlimGates()
	if err != nil {
		t.Fatal(err)
	}
	if len(required) != 2 {
		t.Fatalf("SlimGates = %+v; want 2 required jobs", required)
	}
	if required[0].Workflow != "test.yml" || required[0].Job != "merge-group-slim" {
		t.Fatalf("SlimGates[0] = %+v; want test.yml/merge-group-slim", required[0])
	}
	if required[1].Workflow != "static-contract-gates.yml" || required[1].Job != "Verify slim-b gate" {
		t.Fatalf("SlimGates[1] = %+v; want static-contract-gates.yml/Verify slim-b gate", required[1])
	}
}

// TestPathSelectionUnchangedBySlimLane pins the pull-request (path) selection
// contract the slim lane must not alter: the same paths select the same gates
// and jobs before and after the lane lands.
func TestPathSelectionUnchangedBySlimLane(t *testing.T) {
	t.Parallel()
	reg := loadSlimFixture(t)
	got, err := SelectQueueWorkflow([]*Registry{reg}, []string{"go/internal/graph/a.go"}, false, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"path-gate"}) || !reflect.DeepEqual(got.Jobs, []string{"go-core"}) {
		t.Fatalf("path selection = %+v; want path-gate/go-core", got)
	}
	required, err := reg.RequiredGates([]string{"go/internal/graph/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(required) != 1 || required[0].Job != "go-core" {
		t.Fatalf("RequiredGates = %+v; want one go-core job", required)
	}
}
