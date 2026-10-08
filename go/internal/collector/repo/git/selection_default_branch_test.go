// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	if !r.delta.IsEmpty() || r.delta.BaselineCommitSHA != "" {
		t.Fatalf("delta = %+v; a default-branch change must take a full snapshot", r.delta)
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

// TestUpdateRepositoryRemoteHeadUnresolved keeps syncing a repository whose
// remote HEAD names no branch: the HEAD probe is dropped and the tracked
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
	if r.sourceSHA != newTip || r.delta.BaselineCommitSHA != f.oldTip {
		t.Fatalf("sourceSHA=%q baseline=%q; want %q as a delta from %q", r.sourceSHA, r.delta.BaselineCommitSHA, newTip, f.oldTip)
	}
	if strings.Contains(r.logs.String(), defaultBranchChangedMsg) {
		t.Fatalf("unresolved remote HEAD logged a change:\n%s", r.logs.String())
	}
}
