// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesTrackedIgnoredFile
// proves the issue #5591 fix at site 2 (the filesystem managed-copy path):
// copyRepositoryTree resolves gitTrackedFiles against sourceRoot (the git
// checkout) BEFORE the tree is copied, so a force-committed
// (`git add -f`) file that matches the source repo's own .gitignore rule
// lands in the managed copy, while a genuinely untracked file matching the
// same rule does not.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesTrackedIgnoredFile(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "terraform.tfstate"), "{}")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "scratch.tfstate"), "{}")
	runGit(t, sourceRepo, "add", "main.go", ".gitignore")
	runGit(t, sourceRepo, "add", "-f", "terraform.tfstate")
	runGit(t, sourceRepo, "commit", "-m", "initial")
	// scratch.tfstate is intentionally never `git add`ed.

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-a")
	if _, err := os.Stat(filepath.Join(copiedRoot, "terraform.tfstate")); err != nil {
		t.Fatalf("managed copy missing tracked terraform.tfstate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copiedRoot, "scratch.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains untracked scratch.tfstate (stat err = %v, want IsNotExist)", err)
	}
}

// TestNativeRepositorySelectorSelectRepositoriesFilesystemStillCopiesNoEshuIgnoredTrackedFile
// proves .eshuignore remains the operator's own opt-out in the managed-copy
// path too: it still excludes a file git tracks from the copy, unlike
// .gitignore.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemStillSkipsEshuIgnoredTrackedFile(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-b")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".eshuignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "terraform.tfstate"), "{}")
	runGit(t, sourceRepo, "add", "main.go", ".eshuignore")
	runGit(t, sourceRepo, "add", "-f", "terraform.tfstate")
	runGit(t, sourceRepo, "commit", "-m", "initial")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	selected, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if len(selected.Repositories) != 1 {
		t.Fatalf("len(Repositories) = %d, want 1", len(selected.Repositories))
	}
	wantCommit := runGit(t, sourceRepo, "rev-parse", "HEAD")
	if got := selected.Repositories[0].SourceCommitSHA; got != wantCommit {
		t.Fatalf("SourceCommitSHA = %q, want clean source commit %q", got, wantCommit)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-b")
	if _, err := os.Stat(filepath.Join(copiedRoot, "terraform.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains eshuignored terraform.tfstate (stat err = %v, want IsNotExist)", err)
	}
}

// TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesNestedRepoTrackedIgnoredFile
// is the issue #5658 P1a repro: a nested repository inside a filesystem-source
// checkout (e.g. an embedded/submodule-like repo at modules/nested, its own
// ".git") has its own tracked set, distinct from the outer repo's. The outer
// repo's own `git ls-files` lists the nested repo's gitlink path
// ("modules/nested") but NOT the nested repo's own tracked files
// ("modules/nested/terraform.tfstate") — resolving tracked status only once
// at the outer sourceRoot would silently drop a force-added file inside the
// nested repo whose OWN .gitignore matches it. copyRepositoryTree must
// resolve tracked status against the NEAREST enclosing git root (the nested
// repo itself), not always sourceRoot.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesNestedRepoTrackedIgnoredFile(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-nested")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "*.tfstate\n")
	runGit(t, sourceRepo, "add", "main.go", ".gitignore")
	runGit(t, sourceRepo, "commit", "-m", "outer initial")

	nestedRepo := filepath.Join(sourceRepo, "modules", "nested")
	if err := os.MkdirAll(nestedRepo, 0o755); err != nil {
		t.Fatalf("mkdir nested repo: %v", err)
	}
	mustInitGitRepo(t, nestedRepo)
	writeSelectionTestFile(t, filepath.Join(nestedRepo, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(nestedRepo, "terraform.tfstate"), "{}")
	writeSelectionTestFile(t, filepath.Join(nestedRepo, "scratch.tfstate"), "{}")
	runGit(t, nestedRepo, "add", ".gitignore")
	runGit(t, nestedRepo, "add", "-f", "terraform.tfstate")
	runGit(t, nestedRepo, "commit", "-m", "nested initial")
	// scratch.tfstate is intentionally never `git add`ed inside the nested repo.

	// Records the nested repo as an embedded gitlink in the outer repo's
	// tree — this is what makes the outer repo's own `git ls-files` list
	// "modules/nested" without listing anything beneath it.
	runGit(t, sourceRepo, "add", "modules/nested")
	runGit(t, sourceRepo, "commit", "-m", "add nested gitlink")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-nested")
	if _, err := os.Stat(filepath.Join(copiedRoot, "modules", "nested", "terraform.tfstate")); err != nil {
		t.Fatalf("managed copy missing nested tracked terraform.tfstate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copiedRoot, "modules", "nested", "scratch.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains nested untracked scratch.tfstate (stat err = %v, want IsNotExist)", err)
	}
}

// TestNativeRepositorySelectorSelectRepositoriesFilesystemKeepsNestedUntrackedFileOuterGitignoreMatches
// proves the managed copy stops applying an outer .gitignore across a nested
// repository boundary (issue #5667).
//
// Git never does this. A nested repository has its own ignore scope, so a rule
// in the outer repo's .gitignore says nothing about a path inside the nested
// one. Discovery already behaves that way — it groups by nearest repo root —
// and the managed-copy path did not, so the same file was present or absent
// depending on which path looked at it.
//
// The fixture separates the two scopes deliberately: the outer repo ignores
// "*.tfstate", the nested repo ignores something else entirely, and the
// untracked nested "scratch.tfstate" is matched ONLY by the outer rule. Before
// the fix it was dropped from the copy; git, and discovery, both keep it.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemKeepsNestedUntrackedFileOuterGitignoreMatches(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-scope")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "outer.tfstate"), "{}")
	runGit(t, sourceRepo, "add", "main.go", ".gitignore")
	runGit(t, sourceRepo, "commit", "-m", "outer initial")

	nestedRepo := filepath.Join(sourceRepo, "modules", "nested")
	if err := os.MkdirAll(nestedRepo, 0o755); err != nil {
		t.Fatalf("mkdir nested repo: %v", err)
	}
	mustInitGitRepo(t, nestedRepo)
	// The nested repo's own ignore scope says nothing about *.tfstate.
	writeSelectionTestFile(t, filepath.Join(nestedRepo, ".gitignore"), "*.log\n")
	writeSelectionTestFile(t, filepath.Join(nestedRepo, "nested.go"), "package nested\n")
	writeSelectionTestFile(t, filepath.Join(nestedRepo, "scratch.tfstate"), "{}")
	writeSelectionTestFile(t, filepath.Join(nestedRepo, "debug.log"), "noise")
	runGit(t, nestedRepo, "add", ".gitignore", "nested.go")
	runGit(t, nestedRepo, "commit", "-m", "nested initial")
	// scratch.tfstate and debug.log are both intentionally untracked.

	runGit(t, sourceRepo, "add", "modules/nested")
	runGit(t, sourceRepo, "commit", "-m", "add nested gitlink")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-scope")

	// The whole point: only the OUTER repo ignores this, and it is in the
	// nested repo, so the outer rule does not reach it.
	if _, err := os.Stat(filepath.Join(copiedRoot, "modules", "nested", "scratch.tfstate")); err != nil {
		t.Errorf("managed copy dropped nested scratch.tfstate: %v; the outer repo's "+
			".gitignore must not match across a nested-repo boundary", err)
	}
	// The nested repo's OWN rule still applies, or the fix would have replaced
	// one wrong scope with no scope at all.
	if _, err := os.Stat(filepath.Join(copiedRoot, "modules", "nested", "debug.log")); !os.IsNotExist(err) {
		t.Errorf("managed copy kept nested debug.log (stat err = %v, want IsNotExist); "+
			"the nested repo's own .gitignore still governs its own files", err)
	}
	// And the outer repo's rule still applies to the outer repo's own files.
	if _, err := os.Stat(filepath.Join(copiedRoot, "outer.tfstate")); !os.IsNotExist(err) {
		t.Errorf("managed copy kept outer.tfstate (stat err = %v, want IsNotExist)", err)
	}
}

// TestFingerprintTreeScopesIgnoreToNestedRepo proves the fingerprint applies the
// same nearest-root ignore scope the managed copy does (issue #5667 review).
//
// The two must agree or the copy goes stale. fingerprintTree decides whether
// syncFilesystemRepositories bothers to recopy: if the fingerprint ignores a
// file the copy keeps, then editing ONLY that file leaves the fingerprint
// unchanged, sync returns early, and the managed copy and the emitted
// generation both keep the old content indefinitely.
//
// Scoping only the copy — as the first version of this change did — is what
// creates that disagreement. Before it, both paths excluded the file and stayed
// consistent while being wrong together.
func TestFingerprintTreeScopesIgnoreToNestedRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustInitGitRepo(t, root)
	writeSelectionTestFile(t, filepath.Join(root, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(root, "main.go"), "package main\n")

	nested := filepath.Join(root, "modules", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	mustInitGitRepo(t, nested)
	writeSelectionTestFile(t, filepath.Join(nested, ".gitignore"), "*.log\n")
	nestedFile := filepath.Join(nested, "scratch.tfstate")
	writeSelectionTestFile(t, nestedFile, "{}")

	before, err := fingerprintTree(root)
	if err != nil {
		t.Fatalf("fingerprintTree() error = %v", err)
	}

	// Change ONLY the nested file the outer .gitignore matches.
	writeSelectionTestFile(t, nestedFile, "{\"changed\":true}")

	after, err := fingerprintTree(root)
	if err != nil {
		t.Fatalf("fingerprintTree() error = %v", err)
	}

	if before == after {
		t.Error("fingerprint unchanged after editing a nested-repo file that only the " +
			"outer .gitignore matches; the managed copy keeps that file, so sync would " +
			"skip the recopy and leave the copy and emitted generation stale")
	}
}
