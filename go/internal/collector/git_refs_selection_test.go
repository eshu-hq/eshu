// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSelectRepositoriesFilesystemCarriesGitRefsThroughSymlinkedReposDir
// proves the refs map key matches when ESHU_REPOS_DIR is reached through a
// symlink — the golden-corpus gate's mktemp work dir is /var/folders/... on
// macOS, which is a symlink to /private/var/folders/.... Config load resolves
// FilesystemRoot through EvalSymlinks but ReposDir stays unresolved, so a key
// built from the raw ReposDir never matches the resolved lookup path.
func TestSelectRepositoriesFilesystemCarriesGitRefsThroughSymlinkedReposDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found in PATH: %v", err)
	}

	realRoot := t.TempDir()
	linkParent := t.TempDir()
	linkRoot := filepath.Join(linkParent, "workdir-link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	filesystemRoot := filepath.Join(linkRoot, "corpus")
	sourceRepo := filepath.Join(filesystemRoot, "tagged-project")
	if err := os.MkdirAll(sourceRepo, 0o750); err != nil {
		t.Fatalf("create source repo dir: %v", err)
	}
	runGit(t, sourceRepo, "init", "-b", "main")
	runGit(t, sourceRepo, "config", "user.email", "test@example.com")
	runGit(t, sourceRepo, "config", "user.name", "Test")
	writeFile(t, sourceRepo, "README.md", "# tagged project")
	runGit(t, sourceRepo, "add", "README.md")
	runGit(t, sourceRepo, "commit", "-m", "initial")
	commitSHA := strings.TrimSpace(runGit(t, sourceRepo, "rev-parse", "HEAD"))
	runGit(t, sourceRepo, "tag", "-a", "v1.0.0", "-m", "annotated", commitSHA)

	// Build the config through the production loader so normalizeFilesystemConfig
	// resolves FilesystemRoot through EvalSymlinks while ReposDir keeps the
	// unresolved symlinked form — the gate's exact condition.
	config, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
		"ESHU_REPO_SOURCE_MODE": "filesystem",
		"ESHU_FILESYSTEM_ROOT":  filesystemRoot,
		"ESHU_REPOS_DIR":        filepath.Join(linkRoot, "repos"),
		"ESHU_GITHUB_ORG":       "acme",
	}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v", err)
	}
	selector := NativeRepositorySelector{
		Config: config,
		Now: func() time.Time {
			return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	refs := batch.Repositories[0].GitRefs
	var tag *GitRef
	for i := range refs {
		if refs[i].Kind == "tag" && refs[i].Name == "v1.0.0" {
			tag = &refs[i]
		}
	}
	if tag == nil {
		t.Fatalf("expected tag v1.0.0 in GitRefs through symlinked ReposDir, got %+v", refs)
	}
	if tag.HeadSHA != commitSHA {
		t.Fatalf("tag HeadSHA = %q, want peeled commit %q", tag.HeadSHA, commitSHA)
	}
}

// TestSelectRepositoriesFilesystemCarriesGitRefs proves that managed
// filesystem mode (the default) discovers git refs from the source path
// (with .git) even though copyRepositoryTree strips dotfiles from the
// target copy. Before the fix, collectLocalRefs ran on the target path
// (no .git -> silent skip -> zero refs -> legacy fallback -> tags: []).
func TestSelectRepositoriesFilesystemCarriesGitRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found in PATH: %v", err)
	}

	// 1. Create a real git repo with an annotated tag.
	filesystemRoot := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "tagged-project")
	if err := os.MkdirAll(sourceRepo, 0o750); err != nil {
		t.Fatalf("create source repo dir: %v", err)
	}
	runGit(t, sourceRepo, "init", "-b", "main")
	runGit(t, sourceRepo, "config", "user.email", "test@example.com")
	runGit(t, sourceRepo, "config", "user.name", "Test")
	writeFile(t, sourceRepo, "README.md", "# tagged project")
	runGit(t, sourceRepo, "add", "README.md")
	runGit(t, sourceRepo, "commit", "-m", "initial")
	commitSHA := strings.TrimSpace(runGit(t, sourceRepo, "rev-parse", "HEAD"))
	runGit(t, sourceRepo, "tag", "-a", "v1.0.0", "-m", "annotated", commitSHA)

	// 2. Set up managed filesystem mode (NOT filesystemDirect).
	reposDir := t.TempDir()
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
			GithubOrg:      "acme",
		},
		Now: func() time.Time {
			return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	// 3. Select repositories.
	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	// 4. Assert: the selected repository carries GitRefs with the tag.
	refs := batch.Repositories[0].GitRefs
	var tagV1 *GitRef
	for i := range refs {
		if refs[i].Kind == "tag" && refs[i].Name == "v1.0.0" {
			tagV1 = &refs[i]
		}
	}
	if tagV1 == nil {
		t.Fatalf("tag 'v1.0.0' not found in GitRefs: %#v", refs)
	}
	if tagV1.HeadSHA != commitSHA {
		t.Fatalf("tag v1.0.0 HeadSHA = %s, want %s (peeled commit)", tagV1.HeadSHA, commitSHA)
	}
	if tagV1.Default {
		t.Fatal("tag Default = true, want false")
	}
}
