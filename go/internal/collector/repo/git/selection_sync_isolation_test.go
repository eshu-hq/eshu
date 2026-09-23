// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newSyncIsolationInstruments builds a real telemetry.Instruments backed by an
// in-memory manual reader, so a test can assert on the counter value the
// production code path actually recorded rather than trusting the call
// happened.
func newSyncIsolationInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("sync-isolation-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return instruments, reader
}

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

// TestSyncGitRepositoriesMetersCloneFailure proves a clone failure (not just
// list_refs) is metered on eshu_dp_git_repo_sync_failures_total, matching the
// counter's own description ("clone, fetch, list_refs"). Before this, only
// list_refs incremented the counter even though the description promised all
// three isolated operations.
func TestSyncGitRepositoriesMetersCloneFailure(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	// No makeManagedRepo call: no .git marker, so syncGitRepositoriesWithLogger
	// takes the clone path.
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*"clone --progress"*)
		echo "fatal: unable to access 'https://github.com/github/org/repo-a.git/': Resolving timed out" 1>&2
		exit 128
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	instruments, reader := newSyncIsolationInstruments(t)
	config := syncIsolationTestConfig(reposDir)
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{Instruments: instruments},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil (an isolated clone failure)", err)
	}
	if len(synced.SelectedRepoPaths) != 0 {
		t.Fatalf("SelectedRepoPaths = %#v, want empty (clone failed)", synced.SelectedRepoPaths)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	got := collectorCounterValue(t, rm, "eshu_dp_git_repo_sync_failures_total", map[string]string{"operation": "clone"})
	if got != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total{operation=clone} = %d, want 1", got)
	}
}

// TestSyncGitRepositoriesMetersFetchFailure is the fetch-path counterpart of
// TestSyncGitRepositoriesMetersCloneFailure: an existing repository whose
// fetch fails must also increment eshu_dp_git_repo_sync_failures_total, with
// operation=fetch.
func TestSyncGitRepositoriesMetersFetchFailure(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		echo "fatal: unable to access 'https://github.com/github/org/repo-a.git/': Resolving timed out" 1>&2
		exit 128
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	instruments, reader := newSyncIsolationInstruments(t)
	config := syncIsolationTestConfig(reposDir)
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{Instruments: instruments},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil (an isolated fetch failure)", err)
	}
	if len(synced.SelectedRepoPaths) != 0 {
		t.Fatalf("SelectedRepoPaths = %#v, want empty (fetch failed)", synced.SelectedRepoPaths)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	got := collectorCounterValue(t, rm, "eshu_dp_git_repo_sync_failures_total", map[string]string{"operation": "fetch"})
	if got != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total{operation=fetch} = %d, want 1", got)
	}
}

// writeFakeGitForRetryProof installs a fake `git` answering the full
// existing-repo delta-baseline sequence (symbolic-ref, fetch, rev-parse,
// cat-file -e, diff, checkout) against a FIXED remote head ("newsha") and a
// FIXED delta baseline ("oldsha"), so the same changeset (one changed file)
// is computed on every call. ls-remote fails for repositories named in
// failListRefsFor and succeeds for everyone else.
func writeFakeGitForRetryProof(t *testing.T, failListRefsFor ...string) {
	t.Helper()
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")

	var failArms string
	for _, id := range failListRefsFor {
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
		printf "newsha\n"
		;;
	*"cat-file -e oldsha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames oldsha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;
` + failArms + `
	*"ls-remote"*)
		printf "ref: refs/heads/main\tHEAD\n"
		printf "newsha\trefs/heads/main\n"
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

// TestSyncGitRepositoriesSkippedRepoRetriesWithUnlostDeltaNextCycle proves the
// retry claim behind the isolation fix: a repository skipped this cycle
// because its list_refs failed is not silently dropped, because the durable
// delta baseline (resolveScopeBaseline, via the injected resolver) reflects
// the last PROJECTED commit, not this cycle's local checkout. Since a skipped
// repo is never added to SelectedRepoPaths, nothing about it is projected
// this cycle, so the resolver's answer is unchanged next cycle — the same
// oldsha..newsha delta is recomputed and delivered once list_refs succeeds.
func TestSyncGitRepositoriesSkippedRepoRetriesWithUnlostDeltaNextCycle(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	var paths []string
	for _, id := range repoIDs {
		paths = append(paths, makeManagedRepo(t, reposDir, id))
	}
	config := syncIsolationTestConfig(reposDir)
	// The resolver stands in for the durable "last projected commit" store.
	// It is never told to advance in this test, because nothing gets
	// projected downstream of a skipped repository — exactly the invariant
	// under test.
	resolver := &stubBaselineResolver{sha: "oldsha"}
	baseline := gitDeltaBaseline{Resolver: resolver}

	// Cycle 1: repo-b's list_refs fails; repo-b must be skipped this cycle.
	writeFakeGitForRetryProof(t, repoIDs[1])
	cycle1, err := syncGitRepositoriesWithLogger(context.Background(), config, repoIDs, discardLogger(), baseline)
	if err != nil {
		t.Fatalf("cycle 1: syncGitRepositoriesWithLogger() error = %v, want nil", err)
	}
	if _, ok := cycle1.DeltaByRepoPath[paths[1]]; ok {
		t.Fatalf("cycle 1: repo-b unexpectedly has a delta despite its list_refs failing: %#v", cycle1.DeltaByRepoPath[paths[1]])
	}
	for _, p := range cycle1.SelectedRepoPaths {
		if p == paths[1] {
			t.Fatalf("cycle 1: SelectedRepoPaths unexpectedly includes skipped repo-b: %#v", cycle1.SelectedRepoPaths)
		}
	}

	// Cycle 2: list_refs now succeeds everywhere. The resolver still answers
	// "oldsha" (untouched, since cycle 1 projected nothing for repo-b), so
	// the SAME oldsha..newsha delta must appear now — proving cycle 1's
	// change was retried, not lost.
	writeFakeGitForRetryProof(t)
	cycle2, err := syncGitRepositoriesWithLogger(context.Background(), config, repoIDs, discardLogger(), baseline)
	if err != nil {
		t.Fatalf("cycle 2: syncGitRepositoriesWithLogger() error = %v, want nil", err)
	}
	delta, ok := cycle2.DeltaByRepoPath[paths[1]]
	if !ok {
		t.Fatalf("cycle 2: no delta for repo-b %q; repo-b's skipped changeset was lost, not retried. SelectedRepoPaths = %#v", paths[1], cycle2.SelectedRepoPaths)
	}
	want := []string{filepath.Join(paths[1], "cmd", "api", "main.go")}
	if !reflect.DeepEqual(delta.ChangedFileTargets, want) {
		t.Fatalf("cycle 2: repo-b ChangedFileTargets = %#v, want %#v (the same changeset skipped in cycle 1)", delta.ChangedFileTargets, want)
	}
}

// waitForMarkerThenCancel polls for markerPath to appear — the fake git
// script creates it immediately before blocking on the target command — and
// cancels ctx the instant it does. This makes mid-flight-cancellation tests
// deterministic: cancellation always lands while the target git command is
// genuinely in flight, never racing against how long the machine took to
// spawn whatever fast commands ran before it (a fixed sleep-then-cancel
// delay was observed to be flaky here: on a loaded machine the preceding
// symbolic-ref/fetch/rev-parse/checkout chain — four subprocess spawns —
// can itself take longer than a short fixed delay, so the cancellation
// lands on one of THOSE instead of the intended target command).
func waitForMarkerThenCancel(t *testing.T, markerPath string, cancel context.CancelFunc) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(markerPath); err == nil {
			cancel()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("marker file %q never appeared within 5s; the target git command may not have run", markerPath)
	cancel()
}

// writeFakeGitForMidFlightCancellation installs a fake `git` that answers
// symbolic-ref/fetch/rev-parse as no-ops/success, but makes `ls-remote`
// touch markerPath and then block (sleep) well past the caller's
// cancellation window, so the process is still running when the test
// cancels ctx and must be killed via runtimecfg.NewProcessGroupCommand's
// Cancel func (SIGKILL of the process group).
func writeFakeGitForMidFlightCancellation(t *testing.T, markerPath string) {
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
	*"rev-parse refs/remotes/origin/main"*)
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"
		;;
	*"ls-remote"*)
		: > "` + markerPath + `"
		sleep 5
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

// TestSyncGitRepositoriesPropagatesCancellationDuringListRefs is the load-
// bearing counterpart to TestSyncGitRepositoriesPropagatesParentCancellation:
// that test cancels ctx BEFORE the loop starts, so it only exercises the
// pre-existing top-of-iteration ctx.Err() check (selection_cli.go) and never
// reaches resolveRepoRefsIsolated's own ctx.Err() branch. This test cancels
// ctx WHILE the single repo's ls-remote is in flight (blocked on a sleeping
// fake git), so only resolveRepoRefsIsolated's check can catch it: without
// that check, a mid-flight cancellation during the LAST (here, only)
// repository's list_refs would be swallowed as an isolated per-repo failure
// (ok=false, fatalErr=nil) and the cycle would return a false empty success
// instead of propagating the shutdown signal.
//
// It also asserts the eshu_dp_git_repo_sync_failures_total counter stays at
// zero: a cancellation is not a sync failure (#7001 review F4).
func TestSyncGitRepositoriesPropagatesCancellationDuringListRefs(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	markerPath := filepath.Join(t.TempDir(), "list-refs-started")
	writeFakeGitForMidFlightCancellation(t, markerPath)

	instruments, reader := newSyncIsolationInstruments(t)
	config := syncIsolationTestConfig(reposDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go waitForMarkerThenCancel(t, markerPath, cancel)

	start := time.Now()
	synced, err := syncGitRepositoriesWithLogger(
		ctx,
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{Instruments: instruments},
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = nil, elapsed=%s, synced=%#v, want a cancellation error propagated from mid-flight list_refs", elapsed, synced)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want errors.Is(err, context.Canceled)", err)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("syncGitRepositoriesWithLogger() took %s, want well under the fake git's 5s sleep (process group kill on cancel should be near-immediate)", elapsed)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_git_repo_sync_failures_total" {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) > 0 {
				t.Fatalf("eshu_dp_git_repo_sync_failures_total has %d datapoint(s) after a cancellation-only run, want 0: %#v", len(sum.DataPoints), sum.DataPoints)
			}
		}
	}
}

// TestSyncGitRepositoriesDoesNotMeterCloneFailureOnCancellation is the F4
// regression for the clone path: a clone killed by parent-context
// cancellation must not increment eshu_dp_git_repo_sync_failures_total
// (operation=clone) — a shutdown is not a sync failure.
func TestSyncGitRepositoriesDoesNotMeterCloneFailureOnCancellation(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	// No makeManagedRepo call: no .git marker, so the clone path is taken.
	markerPath := filepath.Join(t.TempDir(), "clone-started")
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*"clone --progress"*)
		: > "` + markerPath + `"
		sleep 5
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	instruments, reader := newSyncIsolationInstruments(t)
	config := syncIsolationTestConfig(reposDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go waitForMarkerThenCancel(t, markerPath, cancel)

	if _, err := syncGitRepositoriesWithLogger(ctx, config, repoIDs, discardLogger(), gitDeltaBaseline{Instruments: instruments}); err != nil {
		// The clone path stays isolated even on cancellation (pre-existing
		// base behavior, not changed by #7001); a non-nil error here would be
		// a surprise, but is not itself what this test is proving.
		t.Logf("syncGitRepositoriesWithLogger() error = %v (informational; clone-path isolation is pre-existing base behavior)", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_git_repo_sync_failures_total" {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) > 0 {
				t.Fatalf("eshu_dp_git_repo_sync_failures_total has %d datapoint(s) after a canceled clone, want 0: %#v", len(sum.DataPoints), sum.DataPoints)
			}
		}
	}
}

// TestSyncGitRepositoriesDoesNotMeterFetchFailureOnCancellation is the F4
// regression for the fetch path: a fetch killed by parent-context
// cancellation must not increment eshu_dp_git_repo_sync_failures_total
// (operation=fetch).
func TestSyncGitRepositoriesDoesNotMeterFetchFailureOnCancellation(t *testing.T) {
	repoIDs := []string{"github/org/repo-a"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	markerPath := filepath.Join(t.TempDir(), "fetch-started")
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		: > "` + markerPath + `"
		sleep 5
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	instruments, reader := newSyncIsolationInstruments(t)
	config := syncIsolationTestConfig(reposDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go waitForMarkerThenCancel(t, markerPath, cancel)

	if _, err := syncGitRepositoriesWithLogger(ctx, config, repoIDs, discardLogger(), gitDeltaBaseline{Instruments: instruments}); err != nil {
		t.Logf("syncGitRepositoriesWithLogger() error = %v (informational; fetch-path isolation is pre-existing base behavior)", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_git_repo_sync_failures_total" {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) > 0 {
				t.Fatalf("eshu_dp_git_repo_sync_failures_total has %d datapoint(s) after a canceled fetch, want 0: %#v", len(sum.DataPoints), sum.DataPoints)
			}
		}
	}
}
