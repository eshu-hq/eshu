// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeGitForSyncIsolation installs a fake `git` that answers the
// standard existing-repo update sequence (symbolic-ref, fetch, rev-parse,
// checkout) as no-ops/success, and makes `ls-remote` fail for exactly the
// repositories named in failingRepoIDs (matched by substring on the checkout
// path, which embeds the repo ID) with the same shape of error #7001
// observed on ops-qa: a `git ls-remote` non-zero exit after a DNS timeout.
// Every other repository's ls-remote succeeds with one minimal valid ref.
func writeFakeGitForSyncIsolation(t *testing.T, failingRepoIDs ...string) {
	t.Helper()
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")

	var failArms string
	for _, id := range failingRepoIDs {
		failArms += `*"` + id + `"*"ls-remote"*)
		echo "fatal: unable to access 'https://github.com/` + id + `.git/': Resolving timed out after 300018 milliseconds" 1>&2
		exit 128
		;;
`
	}

	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"
		;;
` + failArms + `
	*"ls-remote"*)
		printf "ref: refs/heads/main\tHEAD\n"
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main\n"
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func syncIsolationTestConfig(reposDir string) RepoSyncConfig {
	return RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 1}
}

func makeManagedRepo(t *testing.T, reposDir, repoID string) string {
	t.Helper()
	repoPath := filepath.Join(reposDir, filepath.FromSlash(repoID))
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker for %q: %v", repoID, err)
	}
	return repoPath
}

// TestSyncGitRepositoriesIsolatesOneListRefsFailure is the RED->GREEN
// regression for #7001: on ops-qa, repository 322 of 795's `git ls-remote`
// timed out on DNS resolution, and the pre-fix code returned that error
// immediately from syncGitRepositoriesWithLogger, aborting the entire cycle
// (propagating out through source.Next() -> "collect scope generation" ->
// compositeRunner's composite_runner_fatal path, which killed the whole
// ingester process and canceled every in-flight write on unrelated scopes).
// The fix must isolate the failure to the one repository, log+meter it, and
// still return the other repositories selected, with no error.
//
// This is a table test over failing-repo position (first/middle/last) per
// the isolation replay matrix: a per-repo fault must not depend on where in
// the fleet the failing repo sorts.
func TestSyncGitRepositoriesIsolatesOneListRefsFailure(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b", "github/org/repo-c"}

	for _, failIdx := range []int{0, 1, 2} {
		failIdx := failIdx
		t.Run(repoIDs[failIdx], func(t *testing.T) {
			reposDir := t.TempDir()
			var paths []string
			for _, id := range repoIDs {
				paths = append(paths, makeManagedRepo(t, reposDir, id))
			}
			writeFakeGitForSyncIsolation(t, repoIDs[failIdx])

			config := syncIsolationTestConfig(reposDir)
			synced, err := syncGitRepositoriesWithLogger(
				context.Background(),
				config,
				repoIDs,
				discardLogger(),
				gitDeltaBaseline{},
			)
			if err != nil {
				t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil (one repo's list_refs failure must not fail the cycle)", err)
			}

			wantSelected := map[string]bool{}
			for i, p := range paths {
				if i != failIdx {
					wantSelected[p] = true
				}
			}
			if len(synced.SelectedRepoPaths) != len(wantSelected) {
				t.Fatalf("SelectedRepoPaths = %#v, want the %d non-failing repos %#v", synced.SelectedRepoPaths, len(wantSelected), wantSelected)
			}
			for _, p := range synced.SelectedRepoPaths {
				if !wantSelected[p] {
					t.Fatalf("SelectedRepoPaths unexpectedly includes failed repo path %q", p)
				}
			}
			failedPath := paths[failIdx]
			if _, ok := synced.RefsByRepoPath[failedPath]; ok {
				t.Fatalf("RefsByRepoPath must not carry an entry for the failed repo %q", failedPath)
			}
			for i, p := range paths {
				if i == failIdx {
					continue
				}
				if _, ok := synced.RefsByRepoPath[p]; !ok {
					t.Fatalf("RefsByRepoPath missing entry for non-failing repo %q; isolation must not affect siblings", p)
				}
			}
		})
	}
}

// TestSyncGitRepositoriesAllReposFailListRefs proves the all-failed edge
// case degrades to an empty, error-free selection rather than a partial
// crash or a spurious success signal — a fully unreachable network still
// leaves the ingester able to complete a (no-op) cycle and retry next poll.
func TestSyncGitRepositoriesAllReposFailListRefs(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	for _, id := range repoIDs {
		makeManagedRepo(t, reposDir, id)
	}
	writeFakeGitForSyncIsolation(t, repoIDs...)

	config := syncIsolationTestConfig(reposDir)
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil (all repos failing list_refs is still a completable, empty cycle)", err)
	}
	if len(synced.SelectedRepoPaths) != 0 {
		t.Fatalf("SelectedRepoPaths = %#v, want empty (every repo's list_refs failed)", synced.SelectedRepoPaths)
	}
}

// TestSyncGitRepositoriesRepeatedFailuresAcrossCycles proves the isolation is
// stateless across cycles: a repo that fails list_refs on cycle N is not left
// in some half-selected state that changes cycle N+1's behavior, and the
// SAME repo failing again on the next cycle is isolated identically (natural
// per-cycle retry with no special-casing required by the caller).
func TestSyncGitRepositoriesRepeatedFailuresAcrossCycles(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	for _, id := range repoIDs {
		makeManagedRepo(t, reposDir, id)
	}
	writeFakeGitForSyncIsolation(t, repoIDs[1])
	config := syncIsolationTestConfig(reposDir)

	for cycle := 0; cycle < 2; cycle++ {
		synced, err := syncGitRepositoriesWithLogger(
			context.Background(),
			config,
			repoIDs,
			discardLogger(),
			gitDeltaBaseline{},
		)
		if err != nil {
			t.Fatalf("cycle %d: syncGitRepositoriesWithLogger() error = %v, want nil", cycle, err)
		}
		if len(synced.SelectedRepoPaths) != 1 {
			t.Fatalf("cycle %d: SelectedRepoPaths = %#v, want exactly the one healthy repo", cycle, synced.SelectedRepoPaths)
		}
	}
}

// TestSyncGitRepositoriesPropagatesParentCancellation proves the fix did not
// weaken the genuinely-fatal path: when the shared context is already
// canceled (ingester shutdown) at the moment list_refs fails, that error must
// still propagate as a real error rather than being swallowed as an isolated
// per-repo failure, so shutdown teardown still observes it.
func TestSyncGitRepositoriesPropagatesParentCancellation(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	writeFakeGitForSyncIsolation(t, repoIDs[0])
	config := syncIsolationTestConfig(reposDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Simulate shutdown already in flight before this repo's sync runs.

	_, err := syncGitRepositoriesWithLogger(
		ctx,
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{},
	)
	if err == nil {
		t.Fatal("syncGitRepositoriesWithLogger() error = nil, want a cancellation error to propagate as fatal")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want errors.Is(err, context.Canceled)", err)
	}
}
