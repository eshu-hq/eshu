// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// prunePermissionedWorkflow is the slice of ghcr-buildcache-prune.yml the
// #9632 wiring test reads.
type prunePermissionedWorkflow struct {
	On          map[string]any `yaml:"on"`
	Permissions map[string]any `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []readinessStep   `yaml:"steps"`
	} `yaml:"jobs"`
}

// checkBuildCachePruneWorkflow pins who may run the delete job and how its
// dry-run and missing-package switches are wired (#9632). The job holds
// packages: write, so only a schedule or a manual dispatch may start it, the
// workflow-level token is empty, and a dispatch can ask for a dry run or for
// a missing package to be tolerated. A scheduled run sets neither switch.
func checkBuildCachePruneWorkflow(raw []byte) string {
	var wf prunePermissionedWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return err.Error()
	}
	for trigger := range wf.On {
		if trigger != "schedule" && trigger != "workflow_dispatch" {
			return "the prune holds packages: write, so only schedule and workflow_dispatch may start it, found " + trigger
		}
	}
	if len(wf.Permissions) != 0 {
		return "the workflow-level permissions must stay empty"
	}
	job, ok := wf.Jobs["prune"]
	if !ok || len(wf.Jobs) != 1 {
		return "the workflow must hold exactly the prune job"
	}
	if job.Permissions["packages"] != "write" || job.Permissions["contents"] != "read" || len(job.Permissions) != 2 {
		return "the prune job must hold exactly contents: read and packages: write"
	}
	dispatch, _ := wf.On["workflow_dispatch"].(map[string]any)
	inputs, _ := dispatch["inputs"].(map[string]any)
	for _, name := range []string{"dry_run", "allow_missing"} {
		if _, ok := inputs[name]; !ok {
			return "workflow_dispatch must offer a " + name + " input"
		}
	}
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, "prune-ghcr-buildcache.sh") || strings.Contains(step.Run, "test-prune") {
			continue
		}
		if step.Env["PRUNE_DRY_RUN"] != "${{ inputs.dry_run && '1' || '0' }}" {
			return "PRUNE_DRY_RUN must come from inputs.dry_run and default to 0"
		}
		if step.Env["PRUNE_ALLOW_MISSING"] != "${{ inputs.allow_missing && '1' || '0' }}" {
			return "PRUNE_ALLOW_MISSING must come from inputs.allow_missing and default to 0"
		}
		return ""
	}
	return "no step runs scripts/ci/prune-ghcr-buildcache.sh"
}

// TestBuildCachePruneWorkflowWiring pins the prune workflow and proves each
// rule is load-bearing with a seeded break (#9632).
func TestBuildCachePruneWorkflowWiring(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "ghcr-buildcache-prune.yml"))
	if err != nil {
		t.Fatal(err)
	}
	clean := string(raw)
	if problem := checkBuildCachePruneWorkflow(raw); problem != "" {
		t.Fatalf("ghcr-buildcache-prune.yml: %s", problem)
	}
	mutations := []struct{ name, from, to, want string }{
		{"pull_request trigger", "on:\n  schedule:", "on:\n  pull_request:\n  schedule:", "only schedule and workflow_dispatch"},
		{"workflow-level token", "permissions: {}", "permissions:\n  packages: write", "workflow-level permissions"},
		{"extra job permission", "      packages: write\n", "      packages: write\n      id-token: write\n", "exactly contents: read and packages: write"},
		{"dry run unwired", "${{ inputs.dry_run && '1' || '0' }}", "0", "PRUNE_DRY_RUN"},
		{"dry run defaults to a delete", "${{ inputs.dry_run && '1' || '0' }}", "${{ inputs.dry_run || '0' }}", "PRUNE_DRY_RUN"},
		{"missing package always tolerated", "${{ inputs.allow_missing && '1' || '0' }}", "1", "PRUNE_ALLOW_MISSING"},
		{"no dry_run input", "      dry_run:", "      dry_run_off:", "dry_run input"},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			if !strings.Contains(clean, m.from) {
				t.Fatalf("mutation anchor %q not found", m.from)
			}
			problem := checkBuildCachePruneWorkflow([]byte(strings.Replace(clean, m.from, m.to, 1)))
			if !strings.Contains(problem, m.want) {
				t.Fatalf("want a problem containing %q, got %q", m.want, problem)
			}
		})
	}
}
