// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the ci-gates binary into a temp directory and returns
// its path. It is shared across subtests in this file.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ci-gates")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(repoRoot(t), "go", "cmd", "ci-gates")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal("git rev-parse failed:", err)
	}
	return strings.TrimSpace(string(out))
}

// writeRegistry writes a minimal registry YAML into dir and returns its path.
func writeRegistry(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "ci-gates.v1.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writePathsFile writes one path per line to a temp file and returns the path.
func writePathsFile(t *testing.T, dir string, paths []string) string {
	t.Helper()
	p := filepath.Join(dir, "paths.txt")
	if err := os.WriteFile(p, []byte(strings.Join(paths, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const selectTestRegistry = `version: v1
gates:
  - id: go-lint
    name: Go Lint
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "bash scripts/dev/precommit-go.sh lint"
    ci:
      workflow: test.yml
      job: "Lint Go"
    requirements: [go]
    ci_only_reason: ""
  - id: docs-build-changed
    name: Docs Build Changed
    category: docs
    tier: pre-push
    blocking: true
    triggers:
      - "docs/**"
    local:
      command: "bash scripts/verify-docs-build-changed.sh"
    ci:
      workflow: test.yml
      job: "Verify docs build"
    requirements: [go]
    ci_only_reason: ""
  - id: ci-only-gate
    name: CI Only Gate
    category: hygiene
    tier: pre-pr
    blocking: true
    triggers:
      - "go/**"
    ci:
      workflow: test.yml
      job: "CI gate"
    requirements: [docker]
    ci_only_reason: "needs Docker"
`

func TestSelectSubcommand_JSONOutput(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	dir := t.TempDir()
	reg := writeRegistry(t, dir, selectTestRegistry)
	paths := writePathsFile(t, dir, []string{"go/internal/foo.go"})

	cmd := exec.Command(bin, "select",
		"--registry", reg,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--json",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("select --json failed: %v\nstdout: %s", err, out)
	}

	var result selectJSONOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if result.Tier != "pre-pr" {
		t.Errorf("tier = %q; want %q", result.Tier, "pre-pr")
	}

	selectedIDs := make(map[string]bool)
	for _, s := range result.Selected {
		selectedIDs[s.ID] = true
	}
	if !selectedIDs["go-lint"] {
		t.Error("go-lint should be in selected")
	}

	ciOnlyIDs := make(map[string]bool)
	for _, c := range result.CIOnly {
		ciOnlyIDs[c.ID] = true
	}
	if !ciOnlyIDs["ci-only-gate"] {
		t.Error("ci-only-gate should be in ci_only")
	}
}

func TestSelectSubcommand_PlainOutput(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	dir := t.TempDir()
	reg := writeRegistry(t, dir, selectTestRegistry)
	paths := writePathsFile(t, dir, []string{"go/internal/foo.go"})

	cmd := exec.Command(bin, "select",
		"--registry", reg,
		"--tier", "pre-pr",
		"--paths-from", paths,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("select plain failed: %v\nstdout: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	found := false
	for _, l := range lines {
		if l == "go-lint" {
			found = true
		}
	}
	if !found {
		t.Errorf("go-lint not found in plain output: %s", out)
	}
}

func TestSelectSubcommand_ExplainOutput(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	dir := t.TempDir()
	reg := writeRegistry(t, dir, selectTestRegistry)
	paths := writePathsFile(t, dir, []string{"go/internal/foo.go"})

	cmd := exec.Command(bin, "select",
		"--registry", reg,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--explain",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("select --explain failed: %v\nstdout: %s", err, out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "go-lint") {
		t.Error("explain output should mention go-lint")
	}
	if !strings.Contains(outStr, "ci-only-gate") {
		t.Error("explain output should mention ci-only-gate")
	}
}

const runTestRegistry = `version: v1
gates:
  - id: passing-gate
    name: Passing Gate
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "true"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
  - id: failing-gate
    name: Failing Gate
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers:
      - "go/**"
    local:
      command: "false"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
  - id: advisory-failing-gate
    name: Advisory Failing Gate
    category: hygiene
    tier: pre-commit
    blocking: false
    triggers:
      - "go/**"
    local:
      command: "false"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`

func TestRunSubcommand_AccumulatesAndExitsNonzero(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	dir := t.TempDir()
	reg := writeRegistry(t, dir, runTestRegistry)
	paths := writePathsFile(t, dir, []string{"go/internal/foo.go"})

	cmd := exec.Command(bin, "run",
		"--registry", reg,
		"--tier", "pre-pr",
		"--paths-from", paths,
	)
	out, err := cmd.CombinedOutput()
	// Should exit non-zero because failing-gate is blocking.
	if err == nil {
		t.Fatal("run should exit nonzero when a blocking gate fails, got nil error")
	}
	outStr := string(out)
	// Both gates should have been run (accumulate, not stop-at-first).
	if !strings.Contains(outStr, "passing-gate") {
		t.Errorf("expected passing-gate in output: %s", outStr)
	}
	if !strings.Contains(outStr, "failing-gate") {
		t.Errorf("expected failing-gate in output: %s", outStr)
	}
}

func TestRunSubcommand_AdvisoryDoesNotFailExit(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	dir := t.TempDir()
	// Only advisory gate that fails.
	yaml := `version: v1
gates:
  - id: advisory-only
    name: Advisory Only
    category: hygiene
    tier: pre-commit
    blocking: false
    triggers:
      - "go/**"
    local:
      command: "false"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	reg := writeRegistry(t, dir, yaml)
	paths := writePathsFile(t, dir, []string{"go/internal/foo.go"})

	cmd := exec.Command(bin, "run",
		"--registry", reg,
		"--tier", "pre-pr",
		"--paths-from", paths,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run with only advisory failures should exit 0, got: %v\n%s", err, out)
	}
}

func TestValidateSubcommand_PassesOnRealRegistry(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	root := repoRoot(t)
	regPath := filepath.Join(root, "specs", "ci-gates.v1.yaml")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		t.Skip("specs/ci-gates.v1.yaml not yet committed — skipping")
	}

	cmd := exec.Command(bin, "validate",
		"--registry", regPath,
		"--repo-root", root,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate on real registry failed: %v\n%s", err, out)
	}
}

// TestValidateSubcommand_DriftPassesOnRealRegistry verifies that --drift passes
// against the committed real tree once specs/ci-gates.v1.yaml includes the new
// #4220 schema fields (hygiene_hooks, non_gate_workflows, hook_id).
func TestValidateSubcommand_DriftPassesOnRealRegistry(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	root := repoRoot(t)
	regPath := filepath.Join(root, "specs", "ci-gates.v1.yaml")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		t.Skip("specs/ci-gates.v1.yaml not yet committed — skipping")
	}

	cmd := exec.Command(bin, "validate",
		"--registry", regPath,
		"--repo-root", root,
		"--drift",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate --drift on real registry failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Errorf("expected PASS in output, got: %s", out)
	}
}

// TestValidateSubcommand_DriftCatchesUnregisteredWorkflow verifies that --drift
// reports an error when a workflow file is not registered.
func TestValidateSubcommand_DriftCatchesUnregisteredWorkflow(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)

	// Build a hermetic repo root with one workflow that is not in the registry.
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "name: test\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: []\n"
	if err := os.WriteFile(filepath.Join(wfDir, "orphan.yml"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	reg := writeRegistry(t, dir, `version: v1
gates: []
`)
	cmd := exec.Command(bin, "validate",
		"--registry", reg,
		"--repo-root", root,
		"--drift",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for unregistered workflow, got nil error")
	}
	if !strings.Contains(string(out), "orphan.yml") {
		t.Errorf("expected error mentioning orphan.yml, got: %s", out)
	}
}

// selectJSONOutput and selectJSONEntry are declared in main.go (same package).

// TestParseCategories_RejectsUnknown proves a typo'd category is an error
// rather than a silent no-op that unselects every gate (#4236 codex P2).
func TestParseCategories_RejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := parseCategories("exactnes"); err == nil {
		t.Error("expected error for unknown category 'exactnes', got nil")
	}
	cats, err := parseCategories("exactness,telemetry")
	if err != nil {
		t.Fatalf("valid categories should parse, got: %v", err)
	}
	if len(cats) != 2 {
		t.Errorf("expected 2 categories, got %d", len(cats))
	}
	if got, err := parseCategories(""); err != nil || got != nil {
		t.Errorf("empty should be (nil,nil), got (%v,%v)", got, err)
	}
}
