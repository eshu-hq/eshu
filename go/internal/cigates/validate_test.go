// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
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
	root := buildHermeticRepo(
		t,
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
	root := buildHermeticRepo(
		t,
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
	root := buildHermeticRepo(
		t,
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
	root := buildHermeticRepo(
		t,
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
	root := buildHermeticRepo(
		t,
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

// TestValidate_LiteralTriggerMissingFails proves the #6055 hardening: a
// literal (non-glob) registry trigger naming a file that does not exist on
// disk is a validation error, not a silently ignored entry. Before this
// check, a stale trigger (its target file deleted or renamed outside a
// tracked move) had NO existence check anywhere in Validate — it simply
// never selected the gate again, and nothing said why.
func TestValidate_LiteralTriggerMissingFails(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			// go/internal/collector/git_snapshot_entity_buckets.go does not
			// exist under this hermetic fixture root — a stale literal
			// trigger, the exact shape #6055 item (e) catalogued for 10 real
			// gates in the committed registry.
			Triggers: []string{"go/internal/collector/git_snapshot_entity_buckets.go"},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})
	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Fatal("expected an error for a literal trigger naming a nonexistent file, got none")
	}
	found := false
	for _, err := range errs {
		if err == nil {
			continue
		}
		if got := err.Error(); got != "" &&
			containsAll(got, "openapi-surface", "git_snapshot_entity_buckets.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error naming both the gate id and the missing trigger, got: %v", errs)
	}
}

// TestValidate_LiteralTriggerPresentPasses is the revert-to-green
// counterpart: the same trigger, now pointing at a file that DOES exist,
// produces no error.
func TestValidate_LiteralTriggerPresentPasses(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
	triggerPath := filepath.Join(root, "go", "internal", "collector", "git_snapshot_entity_buckets.go")
	if err := os.MkdirAll(filepath.Dir(triggerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(triggerPath, []byte("package collector\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/collector/git_snapshot_entity_buckets.go"},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})
	errs := reg.Validate(root)
	if len(errs) != 0 {
		t.Errorf("expected no errors for an existing literal trigger, got: %v", errs)
	}
}

// TestValidate_GlobTriggerNeverRequiresExistence proves the existence check
// is scoped to LITERAL triggers only (isLiteralTrigger's own definition,
// shared with checkPathFilterCoverage): a glob trigger matching zero files
// today is a legitimate future-proofing entry, not a stale one, and must
// never be flagged.
func TestValidate_GlobTriggerNeverRequiresExistence(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"go/internal/collector/nothing/matches/this/**"},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})
	errs := reg.Validate(root)
	if len(errs) != 0 {
		t.Errorf("expected no errors for a glob trigger matching nothing, got: %v", errs)
	}
}

func TestValidate_AccumulatesErrors(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
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

// TestValidate_LiteralTriggerEscapingRootFails pins the containment guard added
// for a review finding: filepath.Join cleans its result, so a trigger carrying
// ".." resolves outside the repository (Join("/repo", "../etc/passwd") is
// "/etc/passwd"). Stat-ing that would let a malformed trigger "exist" against an
// unrelated host file and pass the staleness check checkTriggerPathsExist adds.
func TestValidate_LiteralTriggerEscapingRootFails(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"../etc/passwd"},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})

	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Fatal("Validate() returned no errors; a trigger resolving outside the repository root must fail, or a malformed trigger can satisfy the existence check against an unrelated host file")
	}
	var found bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "outside the repository root") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Validate() errors = %v, want one naming the escaping trigger", errs)
	}
}

// TestValidate_LiteralTriggerEscapingRootViaSymlinkFails pins the symlink half
// of the containment guard. isWithinRoot compares paths lexically, but os.Stat
// follows symlinks, so a committed symlink pointing out of the tree lets a
// lexically-contained trigger satisfy the existence check against a host file —
// the staleness check failing open, which is the defect class this gate removes.
// A ".."-free trigger reaches the stat, so the lexical check alone cannot catch
// this.
func TestValidate_LiteralTriggerEscapingRootViaSymlinkFails(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)

	// outside/ stands in for any directory the repo does not own. The symlink
	// is inside the repo and its trigger path carries no "..", so only symlink
	// resolution can tell that "escape/secret.txt" leaves the tree.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("Symlink() error = %v; platform does not support symlinks", err)
	}

	reg := buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{"escape/secret.txt"},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})

	errs := reg.Validate(root)
	var found bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "through a symlink") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Validate() errors = %v, want one reporting the trigger resolving out of the tree through a symlink; without it the trigger stats an unrelated host file and the staleness check passes", errs)
	}
}
