// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestTrustedFragmentsMakePlannerJobRequired(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	fragmentDir := filepath.Join(root, "specs", "ci-pr-workflows.d")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "test.yml"), []byte("name: Build Test\non: pull_request\njobs:\n  pr-plan: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragmentDir, "frontend.yml"), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, err := selectRequiredGates(&cigates.Registry{}, root, []string{"go/a.go"}, false)
	if err != nil || len(selected) != 1 || selected[0].WorkflowName != "Build Test" || selected[0].Job != "pr-plan" {
		t.Fatalf("planner requirement = %#v, err=%v", selected, err)
	}
}

func TestChangedPRPlanDefinitionRefusesDirectReuse(t *testing.T) {
	t.Parallel()
	if !changedPRPlanDefinition([]string{"specs/ci-pr-workflows.d/frontend.yml"}, false) {
		t.Fatal("manifest edit allowed reuse")
	}
	if !changedPRPlanDefinition([]string{"go/a.go"}, true) {
		t.Fatal("truncated paths allowed reuse")
	}
	if changedPRPlanDefinition([]string{"go/a.go"}, false) {
		t.Fatal("ordinary code path blocked reuse")
	}
}
