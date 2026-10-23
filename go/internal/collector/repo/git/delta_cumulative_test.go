// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// cumulativeFixture is a real repository with three commits, F, c1, and C,
// plus a full-depth checkout that has fetched F and c1 (#8780). F is the
// scope's newest activated full generation, c1 the active delta, and C the
// remote head the next sync diffs.
//
//   - F: a, b, c, d, plus untouched fillers g0..g9 (14 files).
//   - c1: modify a, add e, delete d.
//   - C: revert a to F's bytes, delete e, modify b, add f, re-add d with F's
//     bytes.
//
// The cumulative delta from F to C changes b and f only; e is deleted since
// the active commit c1 although F never had it.
type cumulativeFixture struct {
	src, remote, checkout string
	full, active, head    string
}

func newCumulativeFixture(t *testing.T) cumulativeFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "eshu")
	t.Setenv("GIT_AUTHOR_EMAIL", "eshu@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "eshu")
	t.Setenv("GIT_COMMITTER_EMAIL", "eshu@example.com")
	root := t.TempDir()
	f := cumulativeFixture{
		src:      filepath.Join(root, "src"),
		remote:   filepath.Join(root, "remote.git"),
		checkout: filepath.Join(root, "checkout"),
	}
	runGit(t, root, "init", "-q", "-b", "main", f.src)
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFixtureFile(t, filepath.Join(f.src, name), name+" at F\n")
	}
	for i := range 10 {
		writeFixtureFile(t, filepath.Join(f.src, fmt.Sprintf("g%d", i)), "filler\n")
	}
	runGit(t, f.src, "add", "-A")
	runGit(t, f.src, "commit", "-qm", "F")
	f.full = runGit(t, f.src, "rev-parse", "HEAD")

	writeFixtureFile(t, filepath.Join(f.src, "a"), "a at c1\n")
	writeFixtureFile(t, filepath.Join(f.src, "e"), "e at c1\n")
	runGit(t, f.src, "rm", "-q", "d")
	runGit(t, f.src, "add", "-A")
	runGit(t, f.src, "commit", "-qm", "c1")
	f.active = runGit(t, f.src, "rev-parse", "HEAD")

	runGit(t, root, "clone", "-q", "--bare", f.src, f.remote)
	runGit(t, root, "clone", "-q", "file://"+f.remote, f.checkout)

	writeFixtureFile(t, filepath.Join(f.src, "a"), "a at F\n")
	runGit(t, f.src, "rm", "-q", "e")
	writeFixtureFile(t, filepath.Join(f.src, "b"), "b at C\n")
	writeFixtureFile(t, filepath.Join(f.src, "f"), "f at C\n")
	writeFixtureFile(t, filepath.Join(f.src, "d"), "d at F\n")
	runGit(t, f.src, "add", "-A")
	runGit(t, f.src, "commit", "-qm", "C")
	f.head = runGit(t, f.src, "rev-parse", "HEAD")
	runGit(t, f.remote, "fetch", "-q", f.src, "main:main")
	return f
}

// cumulativeTestConfig fetches deep enough to keep F and c1 reachable.
func cumulativeTestConfig() RepoSyncConfig {
	return RepoSyncConfig{CloneDepth: 50, GitAuthMethod: "none"}
}

// chainBaseline is the resolver answer of a chained scope: the active delta
// at c1 and the newest activated full at F.
func (f cumulativeFixture) chainBaseline() scope.DeltaSyncBaseline {
	return scope.DeltaSyncBaseline{ActiveCommitSHA: f.active, ActiveIsDelta: true, NewestActivatedFullCommitSHA: f.full}
}

func (f cumulativeFixture) update(t *testing.T, baseline scope.DeltaSyncBaseline) *updateResult {
	t.Helper()
	r := &updateResult{}
	r.updated, r.delta, r.sourceSHA, r.err = updateRepository(context.Background(), cumulativeTestConfig(), f.checkout, "",
		discardLogger(), baselineTestEvent(), baseline, func(reason string) { r.fallbacks = append(r.fallbacks, reason) }, "")
	return r
}

func (f cumulativeFixture) targets(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(f.checkout, name))
	}
	return out
}

// TestUpdateRepositoryDiffsCumulativeDeltaFromNewestFull is the #8780
// collector proof: a delta over an active delta is diffed from the newest
// activated full (the union base), deletes everything deleted since either
// commit, records the active commit for the #7319 fence, and records the
// union base for the projector.
func TestUpdateRepositoryDiffsCumulativeDeltaFromNewestFull(t *testing.T) {
	f := newCumulativeFixture(t)
	r := f.update(t, f.chainBaseline())
	if r.err != nil || !r.updated {
		t.Fatalf("updateRepository() updated=%v err=%v fallbacks=%v; want a delta update", r.updated, r.err, r.fallbacks)
	}
	if len(r.fallbacks) != 0 {
		t.Fatalf("fallback reasons = %v, want none", r.fallbacks)
	}
	if want := f.targets("b", "f"); !slices.Equal(r.delta.ChangedFileTargets, want) {
		t.Fatalf("ChangedFileTargets = %v, want %v (AMR of F..C: a reverted and d re-added with F's bytes are unchanged from the base)",
			r.delta.ChangedFileTargets, want)
	}
	if want := []string{"e"}; !slices.Equal(r.delta.DeletedRelativePaths, want) {
		t.Fatalf("DeletedRelativePaths = %v, want %v (e was added by the active chain and is absent at C; d is back)",
			r.delta.DeletedRelativePaths, want)
	}
	if r.delta.BaselineCommitSHA != f.active {
		t.Fatalf("BaselineCommitSHA = %q, want the active commit %q (the #7319 fence commit)", r.delta.BaselineCommitSHA, f.active)
	}
	if r.delta.UnionBaseCommitSHA != f.full {
		t.Fatalf("UnionBaseCommitSHA = %q, want the newest activated full %q", r.delta.UnionBaseCommitSHA, f.full)
	}
	if r.sourceSHA != f.head {
		t.Fatalf("sourceCommitSHA = %q, want head %q", r.sourceSHA, f.head)
	}
}

// TestUpdateRepositoryCumulativeDeltaDecisionTable covers the arms of the
// #8780 baseline decision around the cumulative diff.
func TestUpdateRepositoryCumulativeDeltaDecisionTable(t *testing.T) {
	f := newCumulativeFixture(t)
	const missing = "0000000000000000000000000000000000008780"
	for _, tc := range []struct {
		name         string
		baseline     scope.DeltaSyncBaseline
		wantUpdated  bool
		wantFallback []string
	}{
		{
			name:     "active commit equals remote head",
			baseline: scope.DeltaSyncBaseline{ActiveCommitSHA: f.head, ActiveIsDelta: true, NewestActivatedFullCommitSHA: f.full},
		},
		{
			name:        "no activated full",
			baseline:    scope.DeltaSyncBaseline{ActiveCommitSHA: f.active, ActiveIsDelta: true},
			wantUpdated: true, wantFallback: []string{deltaFallbackNoProjectedFull},
		},
		{
			name:        "union base unreachable",
			baseline:    scope.DeltaSyncBaseline{ActiveCommitSHA: f.active, ActiveIsDelta: true, NewestActivatedFullCommitSHA: missing},
			wantUpdated: true, wantFallback: []string{deltaFallbackBaselineUnreachable},
		},
		{
			name:        "active commit unreachable",
			baseline:    scope.DeltaSyncBaseline{ActiveCommitSHA: missing, ActiveIsDelta: true, NewestActivatedFullCommitSHA: f.full},
			wantUpdated: true, wantFallback: []string{deltaFallbackBaselineUnreachable},
		},
		{
			name:        "no active generation",
			wantUpdated: true, wantFallback: []string{deltaFallbackNoProjectedBaseline},
		},
	} {
		r := f.update(t, tc.baseline)
		if r.err != nil || r.updated != tc.wantUpdated {
			t.Fatalf("%s: updateRepository() updated=%v err=%v; want updated=%v", tc.name, r.updated, r.err, tc.wantUpdated)
		}
		if !slices.Equal(r.fallbacks, tc.wantFallback) {
			t.Fatalf("%s: fallback reasons = %v, want %v", tc.name, r.fallbacks, tc.wantFallback)
		}
		if !r.delta.IsEmpty() || r.delta.BaselineCommitSHA != "" || r.delta.UnionBaseCommitSHA != "" {
			t.Fatalf("%s: delta = %+v, want a full snapshot (no paths, no baseline)", tc.name, r.delta)
		}
	}
}

// TestUpdateRepositoryCumulativeDeltaValveTakesFull proves the growth valve:
// a cumulative delta touching at least half the base's files takes a full
// snapshot with the delta_exceeds_base_half reason.
func TestUpdateRepositoryCumulativeDeltaValveTakesFull(t *testing.T) {
	f := newCumulativeFixture(t)
	for i := range 6 {
		writeFixtureFile(t, filepath.Join(f.src, fmt.Sprintf("g%d", i)), "filler changed\n")
	}
	runGit(t, f.src, "add", "-A")
	runGit(t, f.src, "commit", "-qm", "wide")
	runGit(t, f.remote, "fetch", "-q", f.src, "main:main")
	// b, f, e, and six fillers: 9 touched paths against 14 base files.
	r := f.update(t, f.chainBaseline())
	if r.err != nil || !r.updated {
		t.Fatalf("updateRepository() updated=%v err=%v; want a full update", r.updated, r.err)
	}
	if want := []string{deltaFallbackDeltaExceedsBaseHalf}; !slices.Equal(r.fallbacks, want) {
		t.Fatalf("fallback reasons = %v, want %v", r.fallbacks, want)
	}
	if !r.delta.IsEmpty() || r.delta.UnionBaseCommitSHA != "" {
		t.Fatalf("delta = %+v, want a full snapshot", r.delta)
	}
}

// TestSyncGitRepositoriesDiffsChainedDeltaFromResolver drives the chained
// baseline end to end from the resolver through the sync loop and the
// selection builder into the snapshot's union base.
func TestSyncGitRepositoriesDiffsChainedDeltaFromResolver(t *testing.T) {
	f := newCumulativeFixture(t)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		t.Fatalf("create repos dir: %v", err)
	}
	if err := os.Rename(f.checkout, repoPath); err != nil {
		t.Fatalf("move checkout: %v", err)
	}
	baseline := f.chainBaseline()
	resolver := &stubBaselineResolver{baseline: &baseline}
	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 50}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/repo"},
		discardLogger(), gitDeltaBaseline{Resolver: resolver})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	delta, ok := synced.DeltaByRepoPath[repoPath]
	if !ok {
		t.Fatalf("no delta for %q; deltas = %#v", repoPath, synced.DeltaByRepoPath)
	}
	if delta.UnionBaseCommitSHA != f.full || delta.BaselineCommitSHA != f.active {
		t.Fatalf("delta union base = %q, baseline = %q; want %q and %q", delta.UnionBaseCommitSHA, delta.BaselineCommitSHA, f.full, f.active)
	}
	selected := buildSelectedRepositories(config, synced.SelectedRepoPaths, synced.DeltaByRepoPath, nil, nil, nil, nil, nil)
	if len(selected) != 1 || selected[0].DeltaUnionBaseCommitSHA != f.full || selected[0].DeltaBaselineCommitSHA != f.active {
		t.Fatalf("selected = %+v, want one delta selection with union base %q and baseline %q", selected, f.full, f.active)
	}
}
