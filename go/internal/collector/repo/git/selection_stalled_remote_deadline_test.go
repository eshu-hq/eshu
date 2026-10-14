// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// These cells run the real git binary against a local TCP listener, so the
// deadline is proven against the processes production starts (git and its
// git-remote-http helper), not against a shell script.

// counterByOperation sums an int64 counter by its operation label.
func counterByOperation(t *testing.T, reader *metric.ManualReader, name string) map[string]int64 {
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

const deadlineCounterName = "eshu_dp_git_command_deadline_exceeded_total"

const realGitDeadline = 2 * time.Second

// realGitReturnBound is the wall-time ceiling for a call that must fail at its
// deadline: the deadline, process start, and the process-group kill.
const realGitReturnBound = 5 * time.Second

// isolateGitEnv keeps the host's git config, proxies, and prompts out of the
// real-git cells.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	t.Setenv("NO_PROXY", "127.0.0.1")
	t.Setenv("no_proxy", "127.0.0.1")
}

// tcpServer is a listener that accepts connections and discards what it reads.
// With drip == 0 it never writes (a stall); otherwise it sends the headers of a
// git smart-HTTP advertisement and then the first 30 body bytes, 3 bytes every
// drip interval (10 intervals), then closes: a slow transfer that keeps
// delivering bytes but never finishes a valid reply.
type tcpServer struct {
	URL      string
	accepted chan struct{}
	ln       net.Listener
	mu       sync.Mutex
	conns    []net.Conn
}

func startTCPServer(t *testing.T, drip time.Duration) *tcpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &tcpServer{URL: "http://" + ln.Addr().String() + "/x.git", accepted: make(chan struct{}, 64), ln: ln}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()
			select {
			case s.accepted <- struct{}{}:
			default:
			}
			go func() { _, _ = io.Copy(io.Discard, conn) }()
			if drip > 0 {
				go func() {
					defer conn.Close()
					_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/x-git-upload-pack-advertisement\r\nCache-Control: no-cache\r\nConnection: close\r\n\r\n"))
					body := []byte("001e# service=git-upload-pack\n")
					for i := 0; i < len(body); i += 3 {
						time.Sleep(drip)
						if _, writeErr := conn.Write(body[i : i+3]); writeErr != nil {
							return
						}
					}
				}()
			}
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, c := range s.conns {
			_ = c.Close()
		}
	})
	return s
}

func (s *tcpServer) waitAccepted(t *testing.T) {
	t.Helper()
	select {
	case <-s.accepted:
	case <-time.After(realGitReturnBound):
		t.Fatal("git never connected to the listener; the command failed before the network")
	}
}

// gitProcessCount counts running processes whose command line names url: the
// git-remote-http helper carries it, so it proves the child tree.
func gitProcessCount(url string) int {
	out, err := exec.Command("pgrep", "-f", url).Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(out)))
}

func assertNoGitProcessesFor(t *testing.T, url string) {
	t.Helper()
	limit := time.Now().Add(3 * time.Second)
	for gitProcessCount(url) > 0 {
		if time.Now().After(limit) {
			t.Fatalf("git processes for %s are still alive after the deadline; the process group was not killed", url)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func runGitHere(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRealRepo makes a real repository at repoPath whose origin is remoteURL and
// whose origin/HEAD already names main, so the sync reaches its fetch.
func newRealRepo(t *testing.T, repoPath, remoteURL string) {
	t.Helper()
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitHere(t, repoPath, "init", "-q", "-b", "main")
	runGitHere(t, repoPath, "remote", "add", "origin", remoteURL)
	runGitHere(t, repoPath, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
}

// newRealRemote makes a real repository with one commit on main and returns its
// file:// URL, a remote that fetches and lists refs normally.
func newRealRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitHere(t, dir, "init", "-q", "-b", "main")
	runGitHere(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false",
		"commit", "-q", "--allow-empty", "-m", "init")
	return "file://" + dir
}

func assertNoLockFiles(t *testing.T, repoPath string) {
	t.Helper()
	_ = filepath.WalkDir(filepath.Join(repoPath, ".git"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".lock") {
			t.Errorf("killed command left lock file %s", path)
		}
		return nil
	})
}

// realGitConfig builds a sync config around limits. The #8052 deadline cells
// assert one kill per stalled command, so a limits value that leaves
// RetryAttempts unset runs with the retry off; the #8091 cells set it.
func realGitConfig(reposDir string, limits deadline.Limits) RepoSyncConfig {
	if limits.RetryAttempts == 0 {
		limits.RetryAttempts = 1
	}
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = limits
	return config
}

// TestRealGitStalledCommandsHitTheirOwnDeadline stalls each deadline-bound
// operation against a listener that never answers and checks: the call fails
// at its own configured deadline (the other two deadlines are an hour, so only
// the operation's own value can fire), the error names the deadline and the
// elapsed time, the counter holds exactly that operation, and no git process
// from that command survives.
func TestRealGitStalledCommandsHitTheirOwnDeadline(t *testing.T) {
	hour := time.Hour
	cases := []struct {
		name   string
		op     string
		limits deadline.Limits
		run    func(ctx context.Context, config RepoSyncConfig, repoPath, repoID string) error
	}{
		{
			"fetch", deadline.Fetch,
			deadline.Limits{Resolve: hour, Fetch: realGitDeadline, Clone: hour},
			func(ctx context.Context, config RepoSyncConfig, repoPath, _ string) error {
				return gitFetchBranch(ctx, config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
			},
		},
		{
			"ls_remote", deadline.LsRemote,
			deadline.Limits{Resolve: realGitDeadline, Fetch: hour, Clone: hour},
			func(ctx context.Context, config RepoSyncConfig, repoPath, _ string) error {
				_, err := remoteGitRefs(ctx, config, repoPath, "")
				return err
			},
		},
		{
			"resolve", deadline.Resolve,
			deadline.Limits{Resolve: realGitDeadline, Fetch: hour, Clone: hour},
			func(ctx context.Context, config RepoSyncConfig, repoPath, _ string) error {
				_, err := remoteDefaultBranch(ctx, config, repoPath, "")
				return err
			},
		},
		{
			"clone", deadline.Clone,
			deadline.Limits{Resolve: hour, Fetch: hour, Clone: realGitDeadline},
			func(ctx context.Context, config RepoSyncConfig, repoPath, repoID string) error {
				_, err := cloneRepository(ctx, config, repoID, repoPath, "", nil, gitSyncLogEvent{})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateGitEnv(t)
			server := startTCPServer(t, 0)
			reposDir := t.TempDir()
			repoID := "github/org/repo-a"
			repoPath := filepath.Join(reposDir, filepath.FromSlash(repoID))
			if tc.op == deadline.Clone {
				// Rewrite the github URL the clone builds to the listener.
				t.Setenv("GIT_CONFIG_COUNT", "1")
				t.Setenv("GIT_CONFIG_KEY_0", "url."+strings.TrimSuffix(server.URL, "x.git")+".insteadOf")
				t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
			} else {
				newRealRepo(t, repoPath, server.URL)
			}
			instruments, reader := newSyncIsolationInstruments(t)
			ctx := deadline.WithMetrics(context.Background(), instruments)
			config := realGitConfig(reposDir, tc.limits)

			errCh := make(chan error, 1)
			start := time.Now()
			go func() { errCh <- tc.run(ctx, config, repoPath, repoID) }()
			server.waitAccepted(t)
			if gitProcessCount(strings.TrimSuffix(server.URL, "/x.git")) == 0 {
				t.Fatal("no git process names the listener while the command is in flight; the process check would be vacuous")
			}
			var err error
			select {
			case err = <-errCh:
			case <-time.After(realGitReturnBound):
				t.Fatalf("%s did not fail within %s of a %s deadline; the stalled real git was not bounded", tc.name, realGitReturnBound, realGitDeadline)
			}
			elapsed := time.Since(start)

			var deadlineErr *deadline.Error
			if !errors.As(err, &deadlineErr) {
				t.Fatalf("error = %v, want a *deadline.Error", err)
			}
			if deadlineErr.Operation != tc.op || deadlineErr.Limit != realGitDeadline {
				t.Fatalf("deadline error = %+v, want operation %s and limit %s", deadlineErr, tc.op, realGitDeadline)
			}
			if elapsed < realGitDeadline || deadlineErr.Elapsed < realGitDeadline {
				t.Fatalf("failed after %s (error elapsed %s), before its %s deadline", elapsed, deadlineErr.Elapsed, realGitDeadline)
			}
			if !strings.Contains(err.Error(), "exceeded the 2s deadline after 2.") {
				t.Fatalf("error text = %q, want it to name the deadline and the elapsed time", err)
			}
			if got := counterByOperation(t, reader, deadlineCounterName); got[tc.op] != 1 || len(got) != 1 {
				t.Fatalf("%s = %v, want {%s:1}", deadlineCounterName, got, tc.op)
			}
			assertNoGitProcessesFor(t, strings.TrimSuffix(server.URL, "/x.git"))
			if tc.op != deadline.Clone {
				assertNoLockFiles(t, repoPath)
			}
		})
	}
}

// TestRealGitLoopMovesPastStalledFetch is the #8052 end-to-end cell with real
// git: repository A's fetch stalls on a listener, the loop kills it at the
// deadline, logs it as deadline_exceeded, meters it, and syncs repository B
// from a real remote in the same cycle.
func TestRealGitLoopMovesPastStalledFetch(t *testing.T) {
	isolateGitEnv(t)
	server := startTCPServer(t, 0)
	reposDir := t.TempDir()
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	pathA := filepath.Join(reposDir, "github", "org", "repo-a")
	pathB := filepath.Join(reposDir, "github", "org", "repo-b")
	newRealRepo(t, pathA, server.URL)
	newRealRepo(t, pathB, newRealRemote(t))
	instruments, reader := newSyncIsolationInstruments(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	config := realGitConfig(reposDir, deadline.Limits{Fetch: realGitDeadline})

	type result struct {
		sel GitSyncSelection
		err error
	}
	resCh := make(chan result, 1)
	start := time.Now()
	go func() {
		sel, err := syncGitRepositoriesWithLogger(context.Background(), config, repoIDs, logger, gitDeltaBaseline{Instruments: instruments})
		resCh <- result{sel, err}
	}()
	var res result
	select {
	case res = <-resCh:
	case <-time.After(3 * realGitReturnBound):
		t.Fatal("the sync cycle did not finish; one stalled real fetch paused the loop")
	}
	if elapsed := time.Since(start); elapsed < realGitDeadline || elapsed > 2*realGitReturnBound {
		t.Fatalf("cycle took %s, want about the %s deadline of the one stalled fetch", elapsed, realGitDeadline)
	}
	if res.err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil", res.err)
	}
	if len(res.sel.SelectedRepoPaths) != 1 || res.sel.SelectedRepoPaths[0] != pathB {
		t.Fatalf("SelectedRepoPaths = %v, want only %q (A failed, B synced)", res.sel.SelectedRepoPaths, pathB)
	}
	// A killed fetch selects nothing for A: no ref, delta, or source SHA entry,
	// so no generation is built from it; the failure is a log line and counters.
	if _, ok := res.sel.RefsByRepoPath[pathA]; ok || res.sel.SourceCommitSHAByRepoPath[pathA] != "" {
		t.Fatalf("repository A left selection state after a killed fetch: refs=%v sha=%q",
			res.sel.RefsByRepoPath[pathA], res.sel.SourceCommitSHAByRepoPath[pathA])
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {fetch:1}", deadlineCounterName, got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {fetch:1}", got)
	}
	assertNoGitProcessesFor(t, strings.TrimSuffix(server.URL, "/x.git"))

	var failed map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == "git repository sync failed" {
			failed = rec
		}
	}
	if failed == nil {
		t.Fatalf("no 'git repository sync failed' log line in:\n%s", logs.String())
	}
	if failed["failure_class"] != "deadline_exceeded" || failed["deadline_operation"] != "fetch" ||
		failed["deadline_seconds"] != float64(2) || failed["operation"] != "fetch" ||
		failed["repository_index"] != float64(1) {
		t.Fatalf("failed log = %v, want failure_class deadline_exceeded, deadline_operation fetch, deadline_seconds 2, operation fetch, repository_index 1", failed)
	}
	if elapsed, _ := failed["elapsed_seconds"].(float64); elapsed < 2 {
		t.Fatalf("elapsed_seconds = %v, want at least the 2s deadline", failed["elapsed_seconds"])
	}
}

// TestRealGitDripUnderDeadlineIsNotKilled: a server that keeps sending bytes,
// slowly, for longer than the low-speed window but less than the deadline, is
// progressing. Neither the process deadline nor the low-speed abort may fire;
// git fails only because the dripped response is not a valid reply.
func TestRealGitDripUnderDeadlineIsNotKilled(t *testing.T) {
	isolateGitEnv(t)
	server := startTCPServer(t, 250*time.Millisecond)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	config := realGitConfig(reposDir, deadline.Limits{Fetch: 10 * time.Second, LowSpeed: time.Second})

	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		errCh <- gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	}()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(15 * time.Second):
		t.Fatal("the dripped fetch never ended")
	}
	var deadlineErr *deadline.Error
	if errors.As(err, &deadlineErr) {
		t.Fatalf("error = %v, a progressing transfer must not hit the process deadline", err)
	}
	if err == nil || strings.Contains(err.Error(), "too slow") {
		t.Fatalf("error = %v, want git's own invalid-reply error, not a low-speed abort", err)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("the fetch ended after %s, before the %s drip finished; it was cut short", elapsed, 2500*time.Millisecond)
	}
}

// TestRealGitLowSpeedAbortsAStalledTransfer proves the low-speed complement is
// live in real git: with the process deadline far away, a connection that
// delivers nothing is aborted by http.lowSpeedTime, with git's own error.
func TestRealGitLowSpeedAbortsAStalledTransfer(t *testing.T) {
	isolateGitEnv(t)
	server := startTCPServer(t, 0)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	config := realGitConfig(reposDir, deadline.Limits{Fetch: time.Hour, LowSpeed: realGitDeadline})

	errCh := make(chan error, 1)
	start := time.Now()
	go func() {
		errCh <- gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	}()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(realGitReturnBound + 3*time.Second):
		t.Fatal("the stalled transfer was not aborted by http.lowSpeedTime")
	}
	var deadlineErr *deadline.Error
	if errors.As(err, &deadlineErr) || err == nil {
		t.Fatalf("error = %v, want git's own low-speed abort, not the process deadline", err)
	}
	if elapsed := time.Since(start); elapsed < realGitDeadline {
		t.Fatalf("aborted after %s, before the %s low-speed window", elapsed, realGitDeadline)
	}
	assertNoGitProcessesFor(t, strings.TrimSuffix(server.URL, "/x.git"))
}
