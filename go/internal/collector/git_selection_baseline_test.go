// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// writeFakeGitForBaseline installs a fake `git` on PATH whose behavior is
// scripted from the supplied case body. Callers append shell case-arms; the
// scaffold already answers branch resolution and fetch as no-ops.
func writeFakeGitForBaseline(t *testing.T, caseArms string) {
	t.Helper()
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		;;
` + caseArms + `
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func baselineTestConfig() RepoSyncConfig {
	return RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"}
}

func baselineTestEvent() gitSyncLogEvent {
	return gitSyncLogEventFor("example/private-service", 1, 1)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

var errStubResolver = errors.New("stub resolver failure")

type stubBaselineResolver struct {
	sha          string
	err          error
	scopeIDs     []string
	lastFull     time.Time
	lastFullOK   bool
	lastFullErr  error
	fullScopeIDs []string
}

func (s *stubBaselineResolver) LastProjectedCommitSHA(_ context.Context, scopeID string) (string, error) {
	s.scopeIDs = append(s.scopeIDs, scopeID)
	return s.sha, s.err
}

func (s *stubBaselineResolver) LastFullProjectionAt(_ context.Context, scopeID string) (time.Time, bool, error) {
	s.fullScopeIDs = append(s.fullScopeIDs, scopeID)
	return s.lastFull, s.lastFullOK, s.lastFullErr
}

// TestGitScopeIDForManagedRepoMatchesSnapshotScope pins the correctness link of
// epic #2340: the scope ID the sync derives to look up a baseline must equal
// the scope ID the snapshot path persists the generation under. If these drift,
// every lookup misses and the baseline silently never applies.
func TestGitScopeIDForManagedRepoMatchesSnapshotScope(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none"}

	got := gitScopeIDForManagedRepo(config, repoPath)

	managedRepoID := repoIDFromManagedPath(reposDir, repoPath)
	metadata, err := repositoryidentity.MetadataFor(filepath.Base(repoPath), repoPath, repoRemoteURL(config, managedRepoID))
	if err != nil {
		t.Fatalf("MetadataFor() error = %v", err)
	}
	want := buildScope(metadata).ScopeID
	if got != want {
		t.Fatalf("scope id = %q, want %q", got, want)
	}
	if got == "" {
		t.Fatal("derived scope id is empty")
	}
}

// TestSyncGitRepositoriesBaselinesDeltaFromResolver proves end to end that the
// git sync reads the delta baseline from the resolver (the generation
// lifecycle) and diffs against that commit. The fake git only emits a diff when
// asked for the resolver's SHA, so a non-empty delta proves the baseline flowed
// from the resolver into the diff.
func TestSyncGitRepositoriesBaselinesDeltaFromResolver(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e basesha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames basesha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 1}
	resolver := &stubBaselineResolver{sha: "basesha"}
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		[]string{"github/org/repo"},
		discardLogger(),
		gitDeltaBaseline{Resolver: resolver},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}

	delta, ok := synced.DeltaByRepoPath[repoPath]
	if !ok {
		t.Fatalf("no delta for %q; deltas = %#v", repoPath, synced.DeltaByRepoPath)
	}
	want := []string{filepath.Join(repoPath, "cmd", "api", "main.go")}
	if !reflect.DeepEqual(delta.ChangedFileTargets, want) {
		t.Fatalf("ChangedFileTargets = %#v, want %#v", delta.ChangedFileTargets, want)
	}
	if len(resolver.scopeIDs) != 1 || resolver.scopeIDs[0] != gitScopeIDForManagedRepo(config, repoPath) {
		t.Fatalf("resolver scope ids = %#v, want one matching the managed scope", resolver.scopeIDs)
	}
}

// TestSyncReEmitsWhenLocalHeadAlreadyAdvancedPastProjection is the integration
// regression for epic #2340. The remote head equals the local working-copy HEAD
// ("newsha") because a prior generation's checkout advanced HEAD, but that
// generation's projection FAILED, so the last projected commit is still
// "oldsha". The pre-fix sync short-circuited on headSHA==remoteSHA and emitted
// nothing, leaving the oldsha..newsha changes unprojected. Baselining on the
// last projected commit must re-emit them.
func TestSyncReEmitsWhenLocalHeadAlreadyAdvancedPastProjection(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	// HEAD == remote ("newsha"); under the old headSHA==remoteSHA branch this is
	// a no-op. The baseline ("oldsha") is what must drive the diff.
	writeFakeGitForBaseline(t, `	*"rev-parse HEAD"*)
		printf "newsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e oldsha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames oldsha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 1}
	resolver := &stubBaselineResolver{sha: "oldsha"}
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		[]string{"github/org/repo"},
		discardLogger(),
		gitDeltaBaseline{Resolver: resolver},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	delta, ok := synced.DeltaByRepoPath[repoPath]
	if !ok {
		t.Fatalf("no delta for %q: the old headSHA==remoteSHA short-circuit would have skipped the unprojected changes", repoPath)
	}
	want := []string{filepath.Join(repoPath, "cmd", "api", "main.go")}
	if !reflect.DeepEqual(delta.ChangedFileTargets, want) {
		t.Fatalf("ChangedFileTargets = %#v, want %#v", delta.ChangedFileTargets, want)
	}
}

// TestSyncFallsBackToFullSnapshotOnResolverError proves a baseline lookup
// failure degrades safely: the repository is still selected (a full snapshot
// generation is produced) rather than silently skipped or delta'd from an
// untrustworthy HEAD.
func TestSyncFallsBackToFullSnapshotOnResolverError(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 1}
	resolver := &stubBaselineResolver{err: errStubResolver}
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		[]string{"github/org/repo"},
		discardLogger(),
		gitDeltaBaseline{Resolver: resolver},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != repoPath {
		t.Fatalf("SelectedRepoPaths = %#v, want full-snapshot selection of %q", synced.SelectedRepoPaths, repoPath)
	}
	if _, ok := synced.DeltaByRepoPath[repoPath]; ok {
		t.Fatalf("resolver error must produce a full snapshot, got a delta: %#v", synced.DeltaByRepoPath[repoPath])
	}
}

// TestUpdateRepositoryBaselinesDeltaOnLastProjectedCommit is the core
// regression for epic #2340: a prior generation's projection failed AFTER its
// checkout advanced local HEAD to "newsha"; the last SUCCESSFULLY projected
// commit is "oldsha"; the remote has not moved. The previous code
// short-circuited on headSHA==remoteSHA and silently skipped the unprojected
// oldsha..newsha changes. Baselining on the last projected commit must re-emit
// them.
func TestUpdateRepositoryBaselinesDeltaOnLastProjectedCommit(t *testing.T) {
	checkoutMarker := filepath.Join(t.TempDir(), "checkout.ok")
	writeFakeGitForBaseline(t, `	*"rev-parse HEAD"*)
		printf "newsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e oldsha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames oldsha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		touch "`+checkoutMarker+`"
		;;`)

	repoPath := t.TempDir()
	updated, delta, _, err := updateRepository(
		context.Background(),
		baselineTestConfig(),
		repoPath,
		"",
		discardLogger(),
		baselineTestEvent(),
		"oldsha",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v", err)
	}
	if !updated {
		t.Fatal("updated = false, want true (baseline differs from remote)")
	}
	if _, statErr := os.Stat(checkoutMarker); statErr != nil {
		t.Fatalf("checkout marker missing: %v", statErr)
	}
	want := []string{filepath.Join(repoPath, "cmd", "api", "main.go")}
	if !reflect.DeepEqual(delta.ChangedFileTargets, want) {
		t.Fatalf("ChangedFileTargets = %#v, want %#v (delta must diff from baseline oldsha, not local HEAD)", delta.ChangedFileTargets, want)
	}
}

// TestUpdateRepositoryNoOpWhenBaselineEqualsRemote: the last projected commit
// already equals the remote head, so there is nothing new to project.
func TestUpdateRepositoryNoOpWhenBaselineEqualsRemote(t *testing.T) {
	writeFakeGitForBaseline(t, `	*"rev-parse HEAD"*)
		printf "newsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e newsha"*)
		exit 0
		;;`)

	updated, delta, _, err := updateRepository(
		context.Background(),
		baselineTestConfig(),
		t.TempDir(),
		"",
		discardLogger(),
		baselineTestEvent(),
		"newsha",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v", err)
	}
	if updated {
		t.Fatal("updated = true, want false (baseline already at remote)")
	}
	if !delta.IsEmpty() {
		t.Fatalf("delta = %#v, want empty", delta)
	}
}

// TestUpdateRepositoryFallsBackToFullSnapshotWhenNoBaseline: a scope with no
// projected generation yet (first projection) has no trustworthy baseline, so
// the sync must take a full snapshot and signal the fallback.
func TestUpdateRepositoryFallsBackToFullSnapshotWhenNoBaseline(t *testing.T) {
	checkoutMarker := filepath.Join(t.TempDir(), "checkout.ok")
	writeFakeGitForBaseline(t, `	*"rev-parse HEAD"*)
		printf "oldsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		touch "`+checkoutMarker+`"
		;;`)

	var fallbacks []string
	updated, delta, _, err := updateRepository(
		context.Background(),
		baselineTestConfig(),
		t.TempDir(),
		"",
		discardLogger(),
		baselineTestEvent(),
		"",
		func(reason string) { fallbacks = append(fallbacks, reason) },
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v", err)
	}
	if !updated {
		t.Fatal("updated = false, want true (full snapshot still produces a generation)")
	}
	if !delta.IsEmpty() {
		t.Fatalf("delta = %#v, want empty (full snapshot)", delta)
	}
	if _, statErr := os.Stat(checkoutMarker); statErr != nil {
		t.Fatalf("checkout marker missing: %v", statErr)
	}
	if want := []string{"no_projected_baseline"}; !reflect.DeepEqual(fallbacks, want) {
		t.Fatalf("fallbacks = %#v, want %#v", fallbacks, want)
	}
}

// TestUpdateRepositoryFallsBackWhenBaselineUnreachable: the recorded baseline
// was pruned by a shallow fetch (or the local tree diverged), so a delta diff
// would be wrong. The sync must take a full snapshot and signal divergence.
func TestUpdateRepositoryFallsBackWhenBaselineUnreachable(t *testing.T) {
	writeFakeGitForBaseline(t, `	*"rev-parse HEAD"*)
		printf "newsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e prunedsha"*)
		exit 1
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	var fallbacks []string
	updated, delta, _, err := updateRepository(
		context.Background(),
		baselineTestConfig(),
		t.TempDir(),
		"",
		discardLogger(),
		baselineTestEvent(),
		"prunedsha",
		func(reason string) { fallbacks = append(fallbacks, reason) },
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v", err)
	}
	if !updated {
		t.Fatal("updated = false, want true (full snapshot fallback)")
	}
	if !delta.IsEmpty() {
		t.Fatalf("delta = %#v, want empty (full snapshot)", delta)
	}
	if want := []string{"baseline_unreachable"}; !reflect.DeepEqual(fallbacks, want) {
		t.Fatalf("fallbacks = %#v, want %#v", fallbacks, want)
	}
}
