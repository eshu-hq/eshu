// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// flakyGitServer is a real smart-HTTP git remote (git http-backend over CGI)
// whose first stallFirst requests never answer. It is the "socket that starts
// answering" the retry proof needs: the same listener stalls an attempt and
// then serves a valid fetch, so the retry is proven against the processes
// production starts, not against a shell shim (#8091).
type flakyGitServer struct {
	URL      string
	requests atomic.Int32
	// Commit is the SHA of the remote's main branch.
	Commit string
}

// startFlakyGitHTTP serves a bare copy of one real commit over smart HTTP.
// The first stallFirst requests block until the client hangs up.
func startFlakyGitHTTP(t *testing.T, stallFirst int32) *flakyGitServer {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found")
	}
	execPathOut, err := exec.Command(gitBin, "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path: %v", err)
	}
	backendPath := filepath.Join(strings.TrimSpace(string(execPathOut)), "git-http-backend")

	work := t.TempDir()
	runGitHere(t, work, "init", "-q", "-b", "main")
	runGitHere(t, work, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false",
		"commit", "-q", "--allow-empty", "-m", "init")
	root := t.TempDir()
	runGitHere(t, root, "clone", "-q", "--bare", work, filepath.Join(root, "x.git"))
	shaOut, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}

	server := &flakyGitServer{Commit: strings.TrimSpace(string(shaOut))}
	backend := &cgi.Handler{
		Path: backendPath,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server.requests.Add(1) <= stallFirst {
			<-r.Context().Done()
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	server.URL = httpServer.URL + "/x.git"
	return server
}

// TestRealGitFetchRetriesAfterDeadlineAndRecovers is the #8091 regression: with
// ESHU_GIT_RETRY_ATTEMPTS=2 the first fetch attempt dies at its deadline
// against a socket that never answers, and the second attempt succeeds once
// the same socket answers. Before the retry existed the first deadline kill was
// the final result and the repository was skipped for the whole sweep.
func TestRealGitFetchRetriesAfterDeadlineAndRecovers(t *testing.T) {
	isolateGitEnv(t)
	server := startFlakyGitHTTP(t, 1)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)

	env := map[string]string{
		"ESHU_REPOS_DIR":                 reposDir,
		"ESHU_REPO_SOURCE_MODE":          "filesystem",
		"ESHU_FILESYSTEM_ROOT":           reposDir,
		"ESHU_GIT_FETCH_TIMEOUT_SECONDS": "2",
		"ESHU_GIT_RETRY_ATTEMPTS":        "2",
	}
	config, err := LoadRepoSyncConfig("test", func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v", err)
	}

	err = gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	if err != nil {
		t.Fatalf("gitFetchBranch() error = %v, want nil: the second attempt must succeed once the socket answers", err)
	}
	got, err := exec.Command("git", "-C", repoPath, "rev-parse", "refs/remotes/origin/main").Output()
	if err != nil {
		t.Fatalf("rev-parse origin/main: %v", err)
	}
	if strings.TrimSpace(string(got)) != server.Commit {
		t.Fatalf("origin/main = %q, want %q", strings.TrimSpace(string(got)), server.Commit)
	}
	if n := server.requests.Load(); n < 2 {
		t.Fatalf("server saw %d requests, want the stalled one plus the retry", n)
	}
}

// realRetryLimits is the limits value of the real-git sync cells: the shared
// 2 s deadline, two attempts, and a short backoff so the cell is not a 3 s
// sleep.
func realRetryLimits() deadline.Limits {
	return deadline.Limits{
		Resolve: time.Minute, Fetch: realGitDeadline, Clone: time.Minute,
		RetryAttempts: 2, RetryBackoff: 50 * time.Millisecond,
	}
}

// TestRealGitSyncRecoversInTheSameSweep is the end-to-end recovery cell: the
// listener stalls the first fetch, the sweep retries it, and the repository
// is selected in the same cycle with one WARN for the failed attempt, one INFO
// for the recovery, and no "git repository sync failed" line.
func TestRealGitSyncRecoversInTheSameSweep(t *testing.T) {
	isolateGitEnv(t)
	server := startFlakyGitHTTP(t, 1)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	instruments, reader := newSyncIsolationInstruments(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	config := realGitConfig(reposDir, realRetryLimits())

	selection, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/repo-a"},
		logger, gitDeltaBaseline{Instruments: instruments})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if len(selection.SelectedRepoPaths) != 1 || selection.SelectedRepoPaths[0] != repoPath {
		t.Fatalf("SelectedRepoPaths = %v, want [%q]: the retried fetch must select the repository this sweep", selection.SelectedRepoPaths, repoPath)
	}
	records := logRecords(t, &logs)
	if got := recordsNamed(records, "git repository sync failed"); len(got) != 0 {
		t.Fatalf("'git repository sync failed' lines = %v, want none after a recovery", got)
	}
	warns := recordsNamed(records, "git command attempt failed")
	if len(warns) != 1 || warns[0]["failure_class"] != "deadline_exceeded" || warns[0]["operation"] != "fetch" ||
		warns[0]["will_retry"] != true || warns[0]["attempt"] != float64(1) || warns[0]["attempts"] != float64(2) {
		t.Fatalf("WARN lines = %v, want one deadline_exceeded fetch attempt 1 of 2 with will_retry=true", warns)
	}
	if got := recordsNamed(records, "git command recovered on attempt 2"); len(got) != 1 {
		t.Fatalf("recovery lines = %v, want one", got)
	}
	if got := counterByLabel(t, reader, "eshu_dp_git_command_attempts_total", "outcome"); got["retried"] != 1 || got["success"] < 1 {
		t.Fatalf("attempts_total by outcome = %v, want retried=1 and a success", got)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got["fetch"] != 1 {
		t.Fatalf("%s = %v, want fetch:1 (the first attempt's kill)", deadlineCounterName, got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); len(got) != 0 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want none", got)
	}
}

// TestRealGitSyncExhaustedAttemptsKeepOneFailureLine is the exhausted cell:
// both attempts stall, so the failure is final with the same class, the
// repository is skipped as before, and the operator's failure count stays one
// "git repository sync failed" line and one sync-failure increment, not one per
// attempt.
func TestRealGitSyncExhaustedAttemptsKeepOneFailureLine(t *testing.T) {
	isolateGitEnv(t)
	server := startFlakyGitHTTP(t, 2)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	instruments, reader := newSyncIsolationInstruments(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	config := realGitConfig(reposDir, realRetryLimits())

	start := time.Now()
	selection, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/repo-a"},
		logger, gitDeltaBaseline{Instruments: instruments})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 2*realGitDeadline {
		t.Fatalf("sweep took %s, want both %s attempts to have run", elapsed, realGitDeadline)
	}
	if len(selection.SelectedRepoPaths) != 0 {
		t.Fatalf("SelectedRepoPaths = %v, want none for a repository whose fetch failed", selection.SelectedRepoPaths)
	}
	records := logRecords(t, &logs)
	failed := recordsNamed(records, "git repository sync failed")
	if len(failed) != 1 || failed[0]["failure_class"] != "deadline_exceeded" || failed[0]["attempts"] != float64(2) {
		t.Fatalf("failure lines = %v, want exactly one deadline_exceeded line with attempts=2", failed)
	}
	warns := recordsNamed(records, "git command attempt failed")
	if len(warns) != 2 || warns[0]["will_retry"] != true || warns[1]["will_retry"] != false {
		t.Fatalf("WARN lines = %v, want two: will_retry true, then false", warns)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {fetch:1}", got)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got["fetch"] != 2 {
		t.Fatalf("%s = %v, want fetch:2 (one kill per attempt)", deadlineCounterName, got)
	}
}
