// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBlockingWorkflow adds a blocking gate workflow beside the trusted
// publisher fixture and registers it as the aggregated source it must be.
func writeBlockingWorkflow(t *testing.T, triggers string) (string, *Registry) {
	t.Helper()
	body := strings.Replace(trustedRequiredWorkflow, `workflows: ["Build Test"]`, `workflows: ["Build Test", "Security Scan"]`, 1)
	root := writeRequiredWorkflowFixture(t, body)
	path := filepath.Join(root, ".github", "workflows", "security.yml")
	if err := os.WriteFile(path, []byte("name: Security Scan\non:\n"+triggers+"jobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := requiredWorkflowRegistry()
	reg.Gates = []Gate{{
		ID:       "security",
		Blocking: true,
		CI:       CI{Workflow: "security.yml", Job: "scan"},
	}}
	return root, reg
}

func TestCheckRequiredStatusWorkflows_RequiresMergeGroupOnEveryBlockingWorkflow(t *testing.T) {
	t.Parallel()

	root, reg := writeBlockingWorkflow(t, "  pull_request:\n")
	errs := checkRequiredStatusWorkflows(root, reg)
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "merge_group") && strings.Contains(err.Error(), "security.yml") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a blocking workflow without a merge_group trigger never reports on a merge-queue commit, "+
			"so the aggregate waits out its timeout on a MISSING check; want a merge_group error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_AcceptsBlockingWorkflowWithMergeGroup(t *testing.T) {
	t.Parallel()

	root, reg := writeBlockingWorkflow(t, "  pull_request:\n  merge_group:\n    types: [checks_requested]\n")
	if errs := checkRequiredStatusWorkflows(root, reg); len(errs) != 0 {
		t.Fatalf("blocking workflow with merge_group returned errors: %v", errs)
	}
}
