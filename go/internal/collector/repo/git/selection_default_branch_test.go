// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

const defaultBranchChangedMsg = `"msg":"git repository default branch changed"`

// defaultBranchFixture is a bare remote whose default branch starts as "old",
// and a shallow single-branch checkout of it, cloned the way cloneRepository
// clones, so the checkout's refs/remotes/origin/HEAD records "old".
type defaultBranchFixture struct {
	src, remote, checkout string
	oldTip                string
}

func newDefaultBranchFixture(t *testing.T) defaultBranchFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "eshu")
	t.Setenv("GIT_AUTHOR_EMAIL", "eshu@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "eshu")
	t.Setenv("GIT_COMMITTER_EMAIL", "eshu@example.com")
	root := t.TempDir()
	f := defaultBranchFixture{
		src:      filepath.Join(root, "src"),
		remote:   filepath.Join(root, "remote.git"),
		checkout: filepath.Join(root, "checkout"),
	}
	runGit(t, root, "init", "-q", "-b", "old", f.src)
	writeFixtureFile(t, filepath.Join(f.src, "app.js"), "old\n")
	runGit(t, f.src, "add", "app.js")
	runGit(t, f.src, "commit", "-qm", "old")
	f.oldTip = runGit(t, f.src, "rev-parse", "HEAD")
	runGit(t, root, "clone", "-q", "--bare", f.src, f.remote)
	runGit(t, root, "clone", "-q", "--depth=1", "--single-branch", "file://"+f.remote, f.checkout)
	if got := runGit(t, f.checkout, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/old" {
		t.Fatalf("fixture origin/HEAD = %q, want refs/remotes/origin/old", got)
	}
	return f
}

// moveDefaultToMain publishes a "main" branch with a new commit and points
// the remote's HEAD at it, returning main's tip.
func (f defaultBranchFixture) moveDefaultToMain(t *testing.T) string {
	t.Helper()
	runGit(t, f.src, "checkout", "-qb", "main")
	writeFixtureFile(t, filepath.Join(f.src, "app.js"), "main\n")
	runGit(t, f.src, "commit", "-qam", "main")
	runGit(t, f.remote, "fetch", "-q", f.src, "main:main")
	runGit(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return runGit(t, f.src, "rev-parse", "HEAD")
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// updateResult is one updateRepository call's outputs plus the fallback
// reasons and log lines it emitted.
type updateResult struct {
	updated   bool
	delta     GitSyncDelta
	sourceSHA string
	err       error
	fallbacks []string
	logs      bytes.Buffer
}

func runUpdate(f defaultBranchFixture, baselineSHA string) *updateResult {
	r := &updateResult{}
	logger := slog.New(slog.NewJSONHandler(&r.logs, nil))
	r.updated, r.delta, r.sourceSHA, r.err = updateRepository(context.Background(), baselineTestConfig(), f.checkout, "",
		logger, baselineTestEvent(), baselineSHA, func(reason string) { r.fallbacks = append(r.fallbacks, reason) })
	return r
}

func assertFollowedMain(t *testing.T, f defaultBranchFixture, r *updateResult, mainTip, detection string) {
	t.Helper()
	if r.err != nil || !r.updated {
		t.Fatalf("updateRepository() updated=%v err=%v; want updated with no error", r.updated, r.err)
	}
	if want := []string{deltaFallbackDefaultBranchChanged}; !slices.Equal(r.fallbacks, want) {
		t.Fatalf("fallback reasons = %v, want %v", r.fallbacks, want)
	}
	if !r.delta.IsEmpty() || r.delta.BaselineCommitSHA != "" || !r.delta.DefaultBranchChanged {
		t.Fatalf("delta = %+v; a default-branch change must take a flagged full snapshot", r.delta)
	}
	if r.sourceSHA != mainTip {
		t.Fatalf("sourceCommitSHA = %q, want main tip %q", r.sourceSHA, mainTip)
	}
	if got := runGit(t, f.checkout, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/main" {
		t.Fatalf("origin/HEAD = %q, want refs/remotes/origin/main", got)
	}
	if got := runGit(t, f.checkout, "rev-parse", "HEAD"); got != mainTip {
		t.Fatalf("checkout HEAD = %q, want main tip %q", got, mainTip)
	}
	out := r.logs.String()
	if !strings.Contains(out, defaultBranchChangedMsg) ||
		!strings.Contains(out, `"previous_branch":"old"`) ||
		!strings.Contains(out, `"branch":"main"`) ||
		!strings.Contains(out, `"detection":"`+detection+`"`) {
		t.Fatalf("missing default-branch-changed WARN with old->main and detection %q; logs:\n%s", detection, out)
	}
}

// TestUpdateRepositoryFollowsDeletedDefaultBranch is the #7678 regression on
// QA: the default branch moved to main and the clone-time branch was deleted,
// so every fetch of the stale origin/HEAD branch failed forever.
func TestUpdateRepositoryFollowsDeletedDefaultBranch(t *testing.T) {
	f := newDefaultBranchFixture(t)
	mainTip := f.moveDefaultToMain(t)
	runGit(t, f.remote, "branch", "-q", "-D", "old")

	assertFollowedMain(t, f, runUpdate(f, f.oldTip), mainTip, defaultBranchDetectionMissingRef)
}

// TestUpdateRepositoryFollowsMovedDefaultBranch covers the silent half of
// #7678: the clone-time branch still exists, so the stale origin/HEAD fetch
// succeeds and the collector keeps indexing the wrong branch.
func TestUpdateRepositoryFollowsMovedDefaultBranch(t *testing.T) {
	f := newDefaultBranchFixture(t)
	mainTip := f.moveDefaultToMain(t)

	assertFollowedMain(t, f, runUpdate(f, f.oldTip), mainTip, defaultBranchDetectionMoved)
}

// TestSyncCarriesDefaultBranchChange proves the cycle carries a default-branch
// change to the default-branch selection only, as a full observation that is
// not a reconciliation: a reconciliation would count the old branch's
// removals as drift on eshu_dp_reconciliation_drift_retractions_total.
func TestSyncCarriesDefaultBranchChange(t *testing.T) {
	f := newDefaultBranchFixture(t)
	mainTip := f.moveDefaultToMain(t)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "big")
	runGit(t, reposDir, "clone", "-q", "--depth=1", "--single-branch", "--branch", "old", "file://"+f.remote, repoPath)
	runGit(t, repoPath, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/old")

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	world := &reconcileWorld{generations: []reconcileWorldGeneration{{
		sha: f.oldTip, status: "active", activated: true, ingestedAt: now.Add(-time.Hour),
	}}}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), reconcileTestConfig(reposDir),
		[]string{"github/org/big"}, discardLogger(), gitDeltaBaseline{
			Resolver:  world,
			Reconcile: reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10},
			Now:       func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if !slices.Contains(synced.SelectedRepoPaths, repoPath) || synced.SourceCommitSHAByRepoPath[repoPath] != mainTip {
		t.Fatalf("selected %v source %q; want %s at main tip %q", synced.SelectedRepoPaths, synced.SourceCommitSHAByRepoPath[repoPath], repoPath, mainTip)
	}
	if len(synced.ReconcileByRepoPath) != 0 {
		t.Fatalf("ReconcileByRepoPath = %v; a default-branch change is not a reconciliation", synced.ReconcileByRepoPath)
	}

	selected := buildSelectedRepositories(reconcileTestConfig(reposDir), synced.SelectedRepoPaths, synced.DeltaByRepoPath,
		synced.ReconcileByRepoPath, synced.SourceCommitSHAByRepoPath, synced.RefsByRepoPath,
		map[string][]RefWorktreeEntry{repoPath: {{WorktreePath: filepath.Join(reposDir, "wt"), Ref: "release", RefKind: "branch"}}})
	if len(selected) != 2 {
		t.Fatalf("selected = %+v, want the default-branch entry and one pinned-ref entry", selected)
	}
	if main := selected[0]; !main.DefaultBranchChanged || main.Delta || main.Reconcile || main.SourceCommitSHA != mainTip {
		t.Fatalf("default-branch entry = %+v; want a flagged full observation at %q that is not a reconciliation", main, mainTip)
	}
	if ref := selected[1]; ref.Ref != "release" || ref.DefaultBranchChanged {
		t.Fatalf("pinned-ref entry = %+v; it must not inherit DefaultBranchChanged", ref)
	}
}

// TestNativeRepositorySnapshotterCarriesDefaultBranchChange keeps the flag on
// its way from the selection to the fact builder.
func TestNativeRepositorySnapshotterCarriesDefaultBranchChange(t *testing.T) {
	t.Parallel()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() = %v", err)
	}
	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: t.TempDir(), DefaultBranchChanged: true,
	})
	if err != nil {
		t.Fatalf("SnapshotRepository() = %v", err)
	}
	if !got.DefaultBranchChanged || got.Reconcile {
		t.Fatalf("snapshot DefaultBranchChanged=%v Reconcile=%v, want true and false", got.DefaultBranchChanged, got.Reconcile)
	}
}

// TestBuildStreamingGenerationDefaultBranchChangeClearsFreshnessHint proves a
// default-branch change bypasses the unchanged-generation skip, whose hint
// does not fold git refs, without marking the repository fact a
// reconciliation.
func TestBuildStreamingGenerationDefaultBranchChangeClearsFreshnessHint(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.DefaultBranchChanged = true

	collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-branch",
		time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), snapshot, false, "")
	if collected.Generation.FreshnessHint != "" || collected.Generation.IsDelta {
		t.Fatalf("generation hint=%q delta=%v; want an empty hint on a full observation",
			collected.Generation.FreshnessHint, collected.Generation.IsDelta)
	}
	repositoryFact := requireRepositoryFact(t, drainFactChannel(collected.Facts))
	if _, ok := repositoryFact.Payload["reconciliation_generation"]; ok {
		t.Fatalf("repository reconciliation_generation = %#v, want absent", repositoryFact.Payload["reconciliation_generation"])
	}
}

// TestUpdateRepositoryUnchangedDefaultBranch keeps the common path: an
// unchanged default branch at the projected commit is a no-op with no
// default-branch log.
func TestUpdateRepositoryUnchangedDefaultBranch(t *testing.T) {
	f := newDefaultBranchFixture(t)

	r := runUpdate(f, f.oldTip)
	if r.err != nil || r.updated || len(r.fallbacks) != 0 {
		t.Fatalf("updateRepository() updated=%v err=%v fallbacks=%v; want a no-op", r.updated, r.err, r.fallbacks)
	}
	if strings.Contains(r.logs.String(), defaultBranchChangedMsg) {
		t.Fatalf("unchanged default branch logged a change:\n%s", r.logs.String())
	}
}

// TestGitMissingRemoteRef pins the matcher to the exact ref git names, not to
// the refspec echoed in the wrapped command line.
func TestGitMissingRemoteRef(t *testing.T) {
	t.Parallel()
	fetchErr := func(stderr string) error {
		return errors.New("git fetch --progress origin +refs/heads/old:refs/remotes/origin/old +HEAD:refs/eshu/remote-head --depth=1: exit status 128: " + stderr)
	}
	for _, tc := range []struct {
		name string
		err  error
		ref  string
		want bool
	}{
		{name: "nil", err: nil, ref: "HEAD", want: false},
		{name: "missing head", err: fetchErr("fatal: couldn't find remote ref HEAD"), ref: "HEAD", want: true},
		{name: "missing branch", err: fetchErr("fatal: couldn't find remote ref refs/heads/old"), ref: "refs/heads/old", want: true},
		{name: "branch is not head", err: fetchErr("fatal: couldn't find remote ref refs/heads/old"), ref: "HEAD", want: false},
		{name: "prefix branch", err: fetchErr("fatal: couldn't find remote ref refs/heads/old2"), ref: "refs/heads/old", want: false},
		{name: "network", err: fetchErr("fatal: unable to access 'https://github.com/o/r/': Could not resolve host"), ref: "refs/heads/old", want: false},
	} {
		if got := gitMissingRemoteRef(tc.err, tc.ref); got != tc.want {
			t.Errorf("%s: gitMissingRemoteRef(%q) = %v, want %v", tc.name, tc.ref, got, tc.want)
		}
	}
}

// TestUpdateRepositoryAdoptFetchFailureKeepsOriginHead proves a failed fetch
// of the new default branch leaves refs/remotes/origin/HEAD on the old branch,
// so the next sync detects the change again instead of tracking a branch it
// never fetched.
func TestUpdateRepositoryAdoptFetchFailureKeepsOriginHead(t *testing.T) {
	binDir := t.TempDir()
	calls := filepath.Join(binDir, "calls.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + calls + `'
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/"*)
		;;
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/old\n"
		;;
	*"fetch --progress origin +refs/heads/old:"*)
		echo "fatal: couldn't find remote ref refs/heads/old" >&2
		exit 128
		;;
	*"ls-remote --symref origin HEAD"*)
		printf "ref: refs/heads/main\tHEAD\nnewsha\tHEAD\n"
		;;
	*"fetch --progress origin +refs/heads/main:"*)
		echo "fatal: unable to access 'https://github.com/o/r/': Could not resolve host" >&2
		exit 128
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	updated, _, _, err := updateRepository(context.Background(), baselineTestConfig(), t.TempDir(), "",
		discardLogger(), baselineTestEvent(), "oldsha", nil)
	if err == nil || updated || !strings.Contains(err.Error(), "Could not resolve host") {
		t.Fatalf("updateRepository() updated=%v err=%v; want the adopt fetch error", updated, err)
	}
	log, readErr := os.ReadFile(calls)
	if readErr != nil {
		t.Fatalf("read fake git calls: %v", readErr)
	}
	if strings.Contains(string(log), "symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/main") {
		t.Fatalf("origin/HEAD was repointed despite the failed fetch; calls:\n%s", log)
	}
}

// TestGitCommandEnvPinsCLocale keeps git messages untranslated: default-branch
// detection and shallow-lock recovery match stderr text. It reads the
// environment a child process receives, so an inherited LC_ALL must not win.
func TestGitCommandEnvPinsCLocale(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	for _, method := range []string{"none", "token", "githubapp", "ssh"} {
		command := exec.Command("env")
		command.Env = gitCommandEnv(RepoSyncConfig{GitAuthMethod: method}, "", "")
		output, err := command.Output()
		if err != nil {
			t.Fatalf("%s: run env: %v", method, err)
		}
		var seen []string
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "LC_ALL=") {
				seen = append(seen, line)
			}
		}
		if !slices.Equal(seen, []string{"LC_ALL=C"}) {
			t.Errorf("%s: child LC_ALL entries = %q, want exactly [LC_ALL=C]", method, seen)
		}
	}
}

// TestUpdateRepositoryRemoteHeadUnresolved keeps syncing a repository whose
// remote HEAD names a missing branch: the HEAD probe is dropped and the tracked
// branch still updates as a delta.
func TestUpdateRepositoryRemoteHeadUnresolved(t *testing.T) {
	f := newDefaultBranchFixture(t)
	writeFixtureFile(t, filepath.Join(f.src, "app.js"), "old v2\n")
	runGit(t, f.src, "commit", "-qam", "old v2")
	newTip := runGit(t, f.src, "rev-parse", "HEAD")
	runGit(t, f.remote, "fetch", "-q", f.src, "old:old")
	runGit(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/does-not-exist")

	r := runUpdate(f, f.oldTip)
	if r.err != nil || !r.updated || len(r.fallbacks) != 0 {
		t.Fatalf("updateRepository() updated=%v err=%v fallbacks=%v; want a delta update", r.updated, r.err, r.fallbacks)
	}
	if r.sourceSHA != newTip || r.delta.BaselineCommitSHA != f.oldTip || r.delta.DefaultBranchChanged {
		t.Fatalf("sourceSHA=%q baseline=%q; want %q as a delta from %q", r.sourceSHA, r.delta.BaselineCommitSHA, newTip, f.oldTip)
	}
	if strings.Contains(r.logs.String(), defaultBranchChangedMsg) {
		t.Fatalf("unresolved remote HEAD logged a change:\n%s", r.logs.String())
	}
}
