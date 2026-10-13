// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// testGitDeadline is the tiny per-command deadline the stall tests set.
	testGitDeadline = 750 * time.Millisecond
	// stallReturnBound is the wall-time ceiling for a call that must return at
	// its deadline. It is far below stallShimSeconds, so a missing deadline
	// shows up as a timeout, and far above the deadline so host load does not.
	stallReturnBound = 10 * time.Second
	stallShimSeconds = "20"
)

// stallShimOptions shapes the fake git installed by writeStallingGit.
type stallShimOptions struct {
	// StallRepo is a substring of the repository whose StallCommand blocks.
	StallRepo string
	// StallCommand is the git subcommand that blocks: fetch, ls-remote, clone.
	StallCommand string
	// Marker, when set, is created the moment a stall begins.
	Marker string
	// SlowCloneSeconds delays every non-stalled clone by that many seconds.
	SlowCloneSeconds string
}

// writeStallingGit installs a fake git that answers the standard existing-repo
// update sequence like writeFakeGitForSyncIsolation, and blocks the configured
// command for the configured repository. Each stalled invocation records its
// shell pid and its sleeping child pid under the returned directory so a test
// can prove the whole process group died.
func writeStallingGit(t *testing.T, opts stallShimOptions) (pidDir string) {
	t.Helper()
	binDir := t.TempDir()
	pidDir = t.TempDir()
	stallArm := ""
	if opts.StallRepo != "" {
		switch opts.StallCommand {
		case "clone":
			stallArm = `*"clone"*"` + opts.StallRepo + `"*) stall ;;`
		default:
			stallArm = `*"` + opts.StallRepo + `"*"` + opts.StallCommand + `"*) stall ;;`
		}
	}
	slow := opts.SlowCloneSeconds
	if slow == "" {
		slow = "0"
	}
	script := `#!/bin/sh
stall() {
	if [ -n "` + opts.Marker + `" ]; then : > "` + opts.Marker + `"; fi
	echo $$ > "` + pidDir + `/sh.$$"
	sleep ` + stallShimSeconds + ` &
	echo $! > "` + pidDir + `/child.$$"
	wait
}
case "$*" in
	` + stallArm + `
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"
		;;
	*"ls-remote"*)
		printf "ref: refs/heads/main\tHEAD\n"
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main\n"
		;;
	*"clone"*)
		for last; do :; done
		sleep ` + slow + `
		mkdir -p "$last/.git"
		;;
	*)
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return pidDir
}

// within runs fn on its own goroutine and fails the test, instead of hanging
// the suite, when fn does not return inside bound.
func within[T any](t *testing.T, bound time.Duration, what string, fn func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- fn() }()
	select {
	case v := <-done:
		return v
	case <-time.After(bound):
		t.Fatalf("%s did not return within %s; the stalled git command was not bounded", what, bound)
		var zero T
		return zero
	}
}

// processGone reports whether pid is no longer a live process. A zombie that
// no reaper has collected yet counts as gone: it is dead and holds nothing.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	paren := strings.LastIndex(s, ")")
	if paren < 0 || paren+2 >= len(s) {
		return false
	}
	return s[paren+2] == 'Z'
}

// assertStallShimGone fails unless the stall shim ran at least once and every
// shell and sleeping child it recorded is dead: the deadline must kill the
// process group, not only the direct child.
func assertStallShimGone(t *testing.T, pidDir string) {
	t.Helper()
	children, err := filepath.Glob(filepath.Join(pidDir, "child.*"))
	if err != nil || len(children) == 0 {
		t.Fatalf("no stalled child recorded in %s (err=%v); the fake git never stalled", pidDir, err)
	}
	shells, _ := filepath.Glob(filepath.Join(pidDir, "sh.*"))
	for _, file := range append(children, shells...) {
		raw, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("read %s: %v", file, readErr)
		}
		pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if convErr != nil {
			t.Fatalf("pid in %s = %q: %v", file, raw, convErr)
		}
		deadline := time.Now().Add(3 * time.Second)
		for !processGone(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d from %s is still alive after the deadline; the process group was not killed", pid, filepath.Base(file))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// counterByOperation sums an int64 counter by its operation label.
func counterByOperation(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				op, _ := dp.Attributes.Value(telemetry.MetricDimensionOperation)
				out[op.AsString()] += dp.Value
			}
		}
	}
	return out
}

const deadlineCounterName = "eshu_dp_git_fetch_deadline_exceeded_total"

func stallTestConfig(reposDir string) RepoSyncConfig {
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = deadline.Limits{Fetch: testGitDeadline, Clone: testGitDeadline}
	return config
}

// TestGitCommandDeadlineKillsStalledFetch drives the shared runner directly.
func TestGitCommandDeadlineKillsStalledFetch(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
	pidDir := writeStallingGit(t, stallShimOptions{StallRepo: "repo-a", StallCommand: "fetch"})
	instruments, reader := newSyncIsolationInstruments(t)
	ctx := deadline.WithMetrics(context.Background(), instruments)
	config := stallTestConfig(reposDir)

	start := time.Now()
	err := within(t, stallReturnBound, "gitRunWithStderrWriter(fetch)", func() error {
		_, runErr := gitRunWithStderrWriter(ctx, repoPath, config, "", nil, "fetch", "origin")
		return runErr
	})
	elapsed := time.Since(start)

	var deadlineErr *deadline.Error
	if !errors.As(err, &deadlineErr) {
		t.Fatalf("error = %v, want a *deadline.Error", err)
	}
	if deadlineErr.Operation != "fetch" || deadlineErr.Command != "fetch" || deadlineErr.Limit != testGitDeadline {
		t.Fatalf("deadline error = %+v, want operation fetch, command fetch, limit %s", deadlineErr, testGitDeadline)
	}
	if deadlineErr.Elapsed < testGitDeadline || deadlineErr.Elapsed > elapsed {
		t.Fatalf("Elapsed = %s, want within [%s, %s]", deadlineErr.Elapsed, testGitDeadline, elapsed)
	}
	for _, want := range []string{"git fetch", "exceeded the 0.75s deadline after "} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error text = %q, want it to contain %q", err.Error(), want)
		}
	}
	assertStallShimGone(t, pidDir)
	if got := counterByOperation(t, reader, deadlineCounterName); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {fetch:1}", deadlineCounterName, got)
	}
}

// TestSyncLoopMovesPastStalledFetch is the #8052 cell: repository A's fetch
// stalls, the loop kills it at the deadline, reports A failed, syncs B, and
// the next cycle would retry A (A is not marked selected or dropped).
func TestSyncLoopMovesPastStalledFetch(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	pathA := makeManagedRepo(t, reposDir, repoIDs[0])
	pathB := makeManagedRepo(t, reposDir, repoIDs[1])
	pidDir := writeStallingGit(t, stallShimOptions{StallRepo: "repo-a", StallCommand: "fetch"})
	instruments, reader := newSyncIsolationInstruments(t)

	synced, err := within(t, stallReturnBound, "syncGitRepositoriesWithLogger", func() syncResult {
		sel, syncErr := syncGitRepositoriesWithLogger(context.Background(), stallTestConfig(reposDir), repoIDs,
			discardLogger(), gitDeltaBaseline{Instruments: instruments})
		return syncResult{sel, syncErr}
	}).unpack()
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil (a stalled fetch is isolated)", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != pathB {
		t.Fatalf("SelectedRepoPaths = %v, want only %q (A=%q failed)", synced.SelectedRepoPaths, pathB, pathA)
	}
	assertStallShimGone(t, pidDir)
	if got := counterByOperation(t, reader, deadlineCounterName); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {fetch:1}", deadlineCounterName, got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {fetch:1}", got)
	}
}

// TestSyncLoopMovesPastStalledClone proves the clone deadline applies to the
// initial clone and that the fetch deadline does not.
func TestSyncLoopMovesPastStalledClone(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	pathB := makeManagedRepo(t, reposDir, repoIDs[1])
	pidDir := writeStallingGit(t, stallShimOptions{StallRepo: "repo-a", StallCommand: "clone"})
	instruments, reader := newSyncIsolationInstruments(t)
	config := stallTestConfig(reposDir)
	config.GitDeadlines.Fetch = time.Hour // a fetch-class deadline must not be what stops the clone

	synced, err := within(t, stallReturnBound, "syncGitRepositoriesWithLogger", func() syncResult {
		sel, syncErr := syncGitRepositoriesWithLogger(context.Background(), config, repoIDs,
			discardLogger(), gitDeltaBaseline{Instruments: instruments})
		return syncResult{sel, syncErr}
	}).unpack()
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != pathB {
		t.Fatalf("SelectedRepoPaths = %v, want only %q", synced.SelectedRepoPaths, pathB)
	}
	assertStallShimGone(t, pidDir)
	if got := counterByOperation(t, reader, deadlineCounterName); got["clone"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {clone:1}", deadlineCounterName, got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["clone"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {clone:1}", got)
	}
}

// TestSlowCloneWithinCloneDeadlineSucceeds is the accuracy guard: a clone
// slower than the fetch deadline but inside the clone deadline is a slow
// clone, not a stall, and must complete.
func TestSlowCloneWithinCloneDeadlineSucceeds(t *testing.T) {
	repoID := "github/org/repo-a"
	reposDir := t.TempDir()
	writeStallingGit(t, stallShimOptions{SlowCloneSeconds: "2"})
	instruments, reader := newSyncIsolationInstruments(t)
	config := stallTestConfig(reposDir)
	config.GitDeadlines = deadline.Limits{Fetch: testGitDeadline, Clone: time.Minute}

	synced, err := within(t, stallReturnBound, "syncGitRepositoriesWithLogger", func() syncResult {
		sel, syncErr := syncGitRepositoriesWithLogger(context.Background(), config, []string{repoID},
			discardLogger(), gitDeltaBaseline{Instruments: instruments})
		return syncResult{sel, syncErr}
	}).unpack()
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil", err)
	}
	checkout, _ := repoCheckoutName(repoID)
	if want := filepath.Join(reposDir, checkout); len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != want {
		t.Fatalf("SelectedRepoPaths = %v, want [%q]; a slow clone inside the clone deadline must succeed", synced.SelectedRepoPaths, want)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); len(got) != 0 {
		t.Fatalf("%s = %v, want no datapoints", deadlineCounterName, got)
	}
}

// TestSyncLoopMovesPastStalledListRefs covers ls-remote: the existing
// list_refs failure label and the new ls_remote deadline label both record.
func TestSyncLoopMovesPastStalledListRefs(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	pathB := makeManagedRepo(t, reposDir, repoIDs[1])
	pidDir := writeStallingGit(t, stallShimOptions{StallRepo: "repo-a", StallCommand: "ls-remote"})
	instruments, reader := newSyncIsolationInstruments(t)

	synced, err := within(t, stallReturnBound, "syncGitRepositoriesWithLogger", func() syncResult {
		sel, syncErr := syncGitRepositoriesWithLogger(context.Background(), stallTestConfig(reposDir), repoIDs,
			discardLogger(), gitDeltaBaseline{Instruments: instruments})
		return syncResult{sel, syncErr}
	}).unpack()
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != pathB {
		t.Fatalf("SelectedRepoPaths = %v, want only %q", synced.SelectedRepoPaths, pathB)
	}
	assertStallShimGone(t, pidDir)
	if got := counterByOperation(t, reader, deadlineCounterName); got["ls_remote"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {ls_remote:1}", deadlineCounterName, got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["list_refs"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {list_refs:1}", got)
	}
}

// TestParentCancelIsNotADeadline proves a shutdown that kills a stalled fetch
// is reported as the context error and never counted as a deadline.
func TestParentCancelIsNotADeadline(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	makeManagedRepo(t, reposDir, repoIDs[0])
	makeManagedRepo(t, reposDir, repoIDs[1])
	marker := filepath.Join(t.TempDir(), "stall-started")
	writeStallingGit(t, stallShimOptions{StallRepo: "repo-a", StallCommand: "fetch", Marker: marker})
	instruments, reader := newSyncIsolationInstruments(t)
	config := stallTestConfig(reposDir)
	config.GitDeadlines.Fetch = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startMarkerCanceler(t, marker, cancel)

	res := within(t, stallReturnBound, "syncGitRepositoriesWithLogger", func() syncResult {
		sel, syncErr := syncGitRepositoriesWithLogger(ctx, config, repoIDs, discardLogger(),
			gitDeltaBaseline{Instruments: instruments})
		return syncResult{sel, syncErr}
	})
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", res.err)
	}
	var deadlineErr *deadline.Error
	if errors.As(res.err, &deadlineErr) {
		t.Fatalf("error = %v, a parent cancel must not be labelled a deadline", res.err)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); len(got) != 0 {
		t.Fatalf("%s = %v after a parent cancel, want no datapoints", deadlineCounterName, got)
	}
}

// TestStaleLockRetryFetchIsAlsoBounded proves the second fetch of the
// shallow.lock recovery path carries its own deadline.
func TestStaleLockRetryFetchIsAlsoBounded(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
	lock := filepath.Join(repoPath, ".git", "shallow.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	binDir := t.TempDir()
	pidDir := t.TempDir()
	counter := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
echo x >> "` + counter + `"
if [ "$(wc -l < "` + counter + `")" -le 1 ]; then
	echo "fatal: Unable to create '` + lock + `': File exists." 1>&2
	exit 128
fi
echo $$ > "` + pidDir + `/sh.$$"
sleep ` + stallShimSeconds + ` &
echo $! > "` + pidDir + `/child.$$"
wait
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := within(t, stallReturnBound, "gitFetchBranch", func() error {
		return gitFetchBranch(context.Background(), stallTestConfig(reposDir), repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	})
	var deadlineErr *deadline.Error
	if !errors.As(err, &deadlineErr) {
		t.Fatalf("error = %v, want the retry fetch to end in a *deadline.Error", err)
	}
	assertStallShimGone(t, pidDir)
}

// syncResult carries both return values of syncGitRepositoriesWithLogger
// through within.
type syncResult struct {
	sel GitSyncSelection
	err error
}

func (r syncResult) unpack() (GitSyncSelection, error) { return r.sel, r.err }
