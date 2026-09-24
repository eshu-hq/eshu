// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	// fakeGitBlockSeconds is how long the fake git's target command sleeps if
	// nothing kills it. It must dwarf both host-load delays and
	// cancelKillBound, so a kill that never lands shows up as an elapsed time
	// near this value rather than as a load-dependent near miss (#7066).
	fakeGitBlockSeconds = "30"
	// cancelKillBound is how long after cancel() the sync call may take to
	// return. Elapsed is measured from cancel(), not from test start: the fake
	// git spawns several shells before the target command, and under heavy
	// load that setup alone can take seconds without saying anything about
	// cancellation latency.
	cancelKillBound = 5 * time.Second
	// markerWait is only a safety net for a fake git that never ran; it is
	// deliberately far above any load-induced spawn delay.
	markerWait = 60 * time.Second
)

// startMarkerCanceler polls for markerPath to appear — the fake git script
// creates it immediately before blocking on the target command — and cancels
// ctx the instant it does. This makes mid-flight-cancellation tests
// deterministic: cancellation always lands while the target git command is
// genuinely in flight, never racing against how long the machine took to
// spawn whatever fast commands ran before it (a fixed sleep-then-cancel delay
// was flaky on a loaded machine).
//
// It returns a func reporting when cancel() was called (zero if it never
// was), so tests can measure how long the kill took from the cancellation
// itself. The poller stops and is waited for in t.Cleanup, so it can never
// report or linger after the test has completed.
func startMarkerCanceler(t *testing.T, markerPath string, cancel context.CancelFunc) (canceledAt func() time.Time) {
	t.Helper()
	var canceledNanos atomic.Int64
	fire := func() {
		canceledNanos.Store(time.Now().UnixNano())
		cancel()
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(markerWait)
		for time.Now().Before(deadline) {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := os.Stat(markerPath); err == nil {
				fire()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Errorf("marker file %q never appeared within %s; the target git command may not have run", markerPath, markerWait)
		fire()
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
	return func() time.Time {
		nanos := canceledNanos.Load()
		if nanos == 0 {
			return time.Time{}
		}
		return time.Unix(0, nanos)
	}
}

// assertKilledPromptlyAfterCancel fails if the sync call took longer than
// cancelKillBound to return after cancel(); returnedAt is captured immediately
// after the call so later assertions cannot inflate the measurement. The fake git blocks for
// fakeGitBlockSeconds, so a process (or orphaned descendant holding its pipes)
// that survives the process-group kill returns only after that long and fails
// here.
func assertKilledPromptlyAfterCancel(t *testing.T, canceledAt func() time.Time, returnedAt time.Time) {
	t.Helper()
	at := canceledAt()
	if at.IsZero() {
		t.Fatal("cancel() was never called; the fake git's target command did not start")
	}
	if sinceCancel := returnedAt.Sub(at); sinceCancel >= cancelKillBound {
		t.Fatalf("syncGitRepositoriesWithLogger() returned %s after cancel(), want under %s (fake git blocks %ss; process group kill on cancel must be near-immediate)", sinceCancel, cancelKillBound, fakeGitBlockSeconds)
	}
}

// assertNoGitRepoSyncFailureDatapoints fails the test if
// eshu_dp_git_repo_sync_failures_total recorded anything: a cancellation is a
// shutdown, not a sync failure (#7001 review F4).
func assertNoGitRepoSyncFailureDatapoints(t *testing.T, reader *sdkmetric.ManualReader, scenario string) {
	t.Helper()
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
				t.Fatalf("eshu_dp_git_repo_sync_failures_total has %d datapoint(s) after %s, want 0: %#v", len(sum.DataPoints), scenario, sum.DataPoints)
			}
		}
	}
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
		sleep ` + fakeGitBlockSeconds + `
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
	canceledAt := startMarkerCanceler(t, markerPath, cancel)

	synced, err := syncGitRepositoriesWithLogger(
		ctx,
		config,
		repoIDs,
		discardLogger(),
		gitDeltaBaseline{Instruments: instruments},
	)
	returnedAt := time.Now()

	if err == nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = nil, synced=%#v, want a cancellation error propagated from mid-flight list_refs", synced)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want errors.Is(err, context.Canceled)", err)
	}
	assertKilledPromptlyAfterCancel(t, canceledAt, returnedAt)

	assertNoGitRepoSyncFailureDatapoints(t, reader, "a cancellation-only run")
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
		sleep ` + fakeGitBlockSeconds + `
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
	canceledAt := startMarkerCanceler(t, markerPath, cancel)

	_, err := syncGitRepositoriesWithLogger(ctx, config, repoIDs, discardLogger(), gitDeltaBaseline{Instruments: instruments})
	returnedAt := time.Now()
	if err != nil {
		// The clone path stays isolated even on cancellation (pre-existing
		// base behavior, not changed by #7001); a non-nil error here would be
		// a surprise, but is not itself what this test is proving.
		t.Logf("syncGitRepositoriesWithLogger() error = %v (informational; clone-path isolation is pre-existing base behavior)", err)
	}
	assertKilledPromptlyAfterCancel(t, canceledAt, returnedAt)

	assertNoGitRepoSyncFailureDatapoints(t, reader, "a canceled clone")
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
		sleep ` + fakeGitBlockSeconds + `
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
	canceledAt := startMarkerCanceler(t, markerPath, cancel)

	_, err := syncGitRepositoriesWithLogger(ctx, config, repoIDs, discardLogger(), gitDeltaBaseline{Instruments: instruments})
	returnedAt := time.Now()
	if err != nil {
		t.Logf("syncGitRepositoriesWithLogger() error = %v (informational; fetch-path isolation is pre-existing base behavior)", err)
	}
	assertKilledPromptlyAfterCancel(t, canceledAt, returnedAt)

	assertNoGitRepoSyncFailureDatapoints(t, reader, "a canceled fetch")
}
