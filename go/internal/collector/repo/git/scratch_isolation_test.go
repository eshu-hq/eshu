// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunGitIgnoresPoisonedGlobalConfig is the #6845 regression: scratch
// repos must not inherit the operator's global git config. On git 2.55 under
// load, a global core.fsmonitor, maintenance.*, or gc.autoDetach keeps
// writing into the scratch .git directory while the test deletes it
// (TempDir RemoveAll / rm -rf failures).
func TestRunGitIgnoresPoisonedGlobalConfig(t *testing.T) {
	poisonDir := t.TempDir()
	poison := filepath.Join(poisonDir, "poisoned-global-gitconfig")
	content := "[user]\n\tname = PoisonedGlobal6845\n[maintenance]\n\tauto = true\n"
	if err := os.WriteFile(poison, []byte(content), 0o600); err != nil {
		t.Fatalf("write poisoned global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", poison)

	repo := t.TempDir()
	mustInitGitRepo(t, repo)
	out := runGit(t, repo, "config", "--list")
	if strings.Contains(out, "PoisonedGlobal6845") {
		t.Fatalf("runGit inherits poisoned global config:\n%s", out)
	}
}

// TestMustInitGitRepoDisablesBackgroundMaintenance pins the #6845 repo-local
// guard: scratch repos must never trigger auto-maintenance/gc, whose detached
// writers race scratch-dir cleanup under load.
func TestMustInitGitRepoDisablesBackgroundMaintenance(t *testing.T) {
	repo := t.TempDir()
	mustInitGitRepo(t, repo)
	if got := runGit(t, repo, "config", "maintenance.auto"); got != "false" {
		t.Fatalf("maintenance.auto = %q, want %q", got, "false")
	}
	if got := runGit(t, repo, "config", "gc.auto"); got != "0" {
		t.Fatalf("gc.auto = %q, want %q", got, "0")
	}
}
