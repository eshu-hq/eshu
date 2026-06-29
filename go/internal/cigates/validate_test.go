// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// buildHermeticRepo creates a temporary directory tree mimicking a repo root
// with specific scripts and workflows present.
func buildHermeticRepo(t *testing.T, scripts []string, workflows []string) string {
	t.Helper()
	root := t.TempDir()
	scriptDir := filepath.Join(root, "scripts")
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range scripts {
		p := filepath.Join(root, s)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/usr/bin/env bash\necho ok\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range workflows {
		p := filepath.Join(wfDir, w)
		if err := os.WriteFile(p, []byte("name: test\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestValidate_AllRefsPresent(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{"scripts/verify-openapi.sh", "scripts/test-verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/query/openapi*.go"},
			Local: &cigates.Local{
				Command:     "bash scripts/verify-openapi.sh",
				TestCommand: "bash scripts/test-verify-openapi.sh",
			},
			CI:           cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
			Requirements: []cigates.Requirement{cigates.ReqGo},
		},
	})
	errs := reg.Validate(root)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// TestValidate_MissingTestCommandScript proves the integrity check also catches
// a renamed or mistyped local.test_command script, not just local.command.
func TestValidate_MissingTestCommandScript(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{"scripts/verify-openapi.sh"}, // command script present, test_command script absent
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/query/openapi*.go"},
			Local: &cigates.Local{
				Command:     "bash scripts/verify-openapi.sh",
				TestCommand: "bash scripts/test-verify-openapi.sh",
			},
			CI:           cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
			Requirements: []cigates.Requirement{cigates.ReqGo},
		},
	})
	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Error("expected error for missing test_command script, got none")
	}
}

func TestValidate_MissingScript(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{}, // no scripts
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/query/openapi*.go"},
			Local: &cigates.Local{
				Command: "bash scripts/verify-openapi.sh",
			},
			CI:           cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
			Requirements: []cigates.Requirement{cigates.ReqGo},
		},
	})
	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Error("expected error for missing script, got none")
	}
}

func TestValidate_MissingWorkflow(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{}, // no workflows
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/query/openapi*.go"},
			Local: &cigates.Local{
				Command: "bash scripts/verify-openapi.sh",
			},
			CI:           cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
			Requirements: []cigates.Requirement{cigates.ReqGo},
		},
	})
	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Error("expected error for missing workflow, got none")
	}
}

func TestValidate_CIOnlySkipsScriptCheck(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{}, // no scripts needed — gate is CI-only
		[]string{"reducer-contention-gate.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:           "reducer-contention",
			Name:         "Reducer Contention Gate",
			Category:     cigates.CategoryRace,
			Tier:         cigates.TierPrePR,
			Blocking:     true,
			Triggers:     []string{"go/internal/storage/postgres/**"},
			Local:        nil,
			CI:           cigates.CI{Workflow: "reducer-contention-gate.yml", Job: "reducer contention gate"},
			Requirements: []cigates.Requirement{cigates.ReqGo, cigates.ReqPostgres},
			CIOnlyReason: "needs Postgres service",
		},
	})
	errs := reg.Validate(root)
	if len(errs) != 0 {
		t.Errorf("CI-only gate with valid workflow should pass, got: %v", errs)
	}
}

func TestValidate_AccumulatesErrors(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(t,
		[]string{}, // missing all scripts
		[]string{}, // missing all workflows
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "gate-a",
			Name:     "Gate A",
			Category: cigates.CategoryHygiene,
			Tier:     cigates.TierPreCommit,
			Blocking: true,
			Triggers: []string{"go/**"},
			Local:    &cigates.Local{Command: "bash scripts/verify-a.sh"},
			CI:       cigates.CI{Workflow: "workflow-a.yml", Job: "job-a"},
		},
		{
			ID:       "gate-b",
			Name:     "Gate B",
			Category: cigates.CategoryHygiene,
			Tier:     cigates.TierPreCommit,
			Blocking: true,
			Triggers: []string{"go/**"},
			Local:    &cigates.Local{Command: "bash scripts/verify-b.sh"},
			CI:       cigates.CI{Workflow: "workflow-b.yml", Job: "job-b"},
		},
	})
	errs := reg.Validate(root)
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors (one per gate), got %d: %v", len(errs), errs)
	}
}
