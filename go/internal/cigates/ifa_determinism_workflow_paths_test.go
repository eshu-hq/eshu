// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ifaDeterminismWorkflow = "ifa-determinism-gate.yml"

// TestIfaDeterminismWorkflowPathsCoverRegistry compares parsed YAML values,
// so block and flow-style paths enforce the same registry/workflow contract.
func TestIfaDeterminismWorkflowPathsCoverRegistry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	filter := readPullRequestFilter(root, ifaDeterminismWorkflow)
	if len(filter.Paths) == 0 || len(filter.Ignore) != 0 {
		t.Fatalf("IFA workflow must declare a nonempty pull_request.paths filter without paths-ignore: %+v", filter)
	}
	for _, missing := range ifaWorkflowMissingGatePaths(reg, filter.Paths) {
		t.Error(missing)
	}
}

// TestIfaDeterminismWorkflowPathDeletionFails proves the semantic check still
// rejects a missing gate trigger when the workflow uses flow-style YAML.
func TestIfaDeterminismWorkflowPathDeletionFails(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wf := "name: IFA\non:\n  pull_request:\n    paths: ['scripts/one.sh']\n"
	if err := os.WriteFile(filepath.Join(workflowDir, ifaDeterminismWorkflow), []byte(wf), 0o644); err != nil {
		t.Fatal(err)
	}
	filter := readPullRequestFilter(root, ifaDeterminismWorkflow)
	for _, tc := range []struct {
		name     string
		blocking bool
	}{
		{name: "blocking", blocking: true},
		{name: "advisory", blocking: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := &Registry{Gates: []Gate{{
				ID: tc.name, Blocking: tc.blocking,
				CI:       CI{Workflow: ifaDeterminismWorkflow, Job: tc.name},
				Triggers: []string{"scripts/one.sh", "scripts/two.sh"},
			}}}
			missing := ifaWorkflowMissingGatePaths(reg, filter.Paths)
			if len(missing) != 1 || !strings.Contains(missing[0], "scripts/two.sh") {
				t.Fatalf("missing-path findings = %v; want scripts/two.sh", missing)
			}
		})
	}
}

func ifaWorkflowMissingGatePaths(reg *Registry, paths []string) []string {
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		pathSet[path] = struct{}{}
	}
	var missing []string
	coveredRows := 0
	for _, gate := range reg.Gates {
		if gate.CI.Workflow != ifaDeterminismWorkflow {
			continue
		}
		coveredRows++
		for _, trigger := range gate.Triggers {
			if _, ok := pathSet[trigger]; !ok {
				missing = append(missing, fmt.Sprintf("%s trigger %q is missing from %s pull_request.paths", gate.ID, trigger, ifaDeterminismWorkflow))
			}
		}
	}
	if coveredRows == 0 {
		missing = append(missing, "registry has no owned IFA workflow gate rows")
	}
	return missing
}
