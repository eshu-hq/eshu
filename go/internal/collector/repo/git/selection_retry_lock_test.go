// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// midPackGitServer is a real smart-HTTP git remote whose first stallPacks
// fetch commands answer their headers, the shallow-info section, and the
// packfile header, then stop. The fetch client has by then taken its shallow lock
// (a depth fetch locks .git/shallow.lock before it reads the pack), so a kill
// at the deadline leaves a real lock file behind: the case a stalled-before-
// answer socket cannot produce.
type midPackGitServer struct {
	URL    string
	Commit string
	posts  atomic.Int32
}

func startMidPackGitHTTP(t *testing.T, stallPacks int32) *midPackGitServer {
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
	// Two commits, so a depth-1 fetch has a shallow boundary to record: the
	// client takes .git/shallow.lock for it. One root commit has none.
	for _, message := range []string{"first", "second"} {
		runGitHere(t, work, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false",
			"commit", "-q", "--allow-empty", "-m", message)
	}
	root := t.TempDir()
	runGitHere(t, root, "clone", "-q", "--bare", work, filepath.Join(root, "x.git"))
	shaOut, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	server := &midPackGitServer{Commit: strings.TrimSpace(string(shaOut))}
	backend := &cgi.Handler{
		Path: backendPath,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request []byte
		if r.Method == http.MethodPost {
			request, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(request))
		}
		// Protocol v2 sends ls-refs and fetch as separate POSTs; only the fetch
		// is stalled mid-response.
		if !bytes.Contains(request, []byte("command=fetch")) || server.posts.Add(1) > stallPacks {
			backend.ServeHTTP(w, r)
			return
		}
		recorded := httptest.NewRecorder()
		backend.ServeHTTP(recorded, r)
		for key, values := range recorded.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(recorded.Code)
		// Send the shallow-info section and the packfile header, not the pack:
		// the client has taken its shallow lock and now waits for pack bytes.
		body := recorded.Body.Bytes()
		cut := len(body)
		if at := bytes.Index(body, []byte("packfile\n")); at >= 0 {
			cut = at + len("packfile\n")
		}
		_, _ = w.Write(body[:cut])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(httpServer.Close)
	server.URL = httpServer.URL + "/x.git"
	return server
}

func midPackFetchConfig(t *testing.T, attempts int) (RepoSyncConfig, string, *midPackGitServer) {
	t.Helper()
	isolateGitEnv(t)
	server := startMidPackGitHTTP(t, 1)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	limits := deadline.Limits{
		Resolve: time.Minute, Fetch: realGitDeadline, Clone: time.Minute,
		RetryAttempts: attempts, RetryBackoff: 50 * time.Millisecond,
	}
	return realGitConfig(reposDir, limits), repoPath, server
}

// TestRealGitFetchKilledMidPackLeavesAShallowLock proves the harness plants a
// real lock: with the retry off, the fetch is killed at its deadline and the
// shallow lock is still there. It is the precondition of the retry cell below.
func TestRealGitFetchKilledMidPackLeavesAShallowLock(t *testing.T) {
	config, repoPath, _ := midPackFetchConfig(t, 1)

	err := gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	var deadlineErr *deadline.Error
	if !errors.As(err, &deadlineErr) {
		t.Fatalf("gitFetchBranch() error = %v, want a *deadline.Error from the mid-pack stall", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoPath, ".git", "shallow.lock")); statErr != nil {
		t.Fatalf("no shallow.lock after the kill (%v); the harness does not plant the lock the retry cell needs", statErr)
	}
}

// TestRealGitFetchRetryClearsTheLockItsKilledAttemptLeft is the lock-recovery
// gap of #8091: the first attempt is killed mid-pack and leaves .git/shallow.lock
// younger than the five-minute stale age, so without the owner-dead path the
// retry fails at once with "File exists" and the repository waits a sweep.
func TestRealGitFetchRetryClearsTheLockItsKilledAttemptLeft(t *testing.T) {
	config, repoPath, server := midPackFetchConfig(t, 2)

	err := gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
	if err != nil {
		t.Fatalf("gitFetchBranch() error = %v, want the retry to succeed once the killed attempt's lock is cleared", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoPath, ".git", "shallow.lock")); !os.IsNotExist(statErr) {
		t.Fatalf("shallow.lock after a recovered fetch: stat error = %v, want it gone", statErr)
	}
	got, outErr := exec.Command("git", "-C", repoPath, "rev-parse", "refs/remotes/origin/main").Output()
	if outErr != nil || strings.TrimSpace(string(got)) != server.Commit {
		t.Fatalf("origin/main = %q (%v), want %q", strings.TrimSpace(string(got)), outErr, server.Commit)
	}
}

// TestKilledAttemptShallowLockRemovalIsScopedToTheAttempt proves the owner-dead
// path removes only a lock created since the killed attempt started. A lock
// that predates the attempt belongs to some other process and is left for the
// stale-age rule, and a missing lock is not an error.
func TestKilledAttemptShallowLockRemovalIsScopedToTheAttempt(t *testing.T) {
	repoPath := t.TempDir()
	gitDir := filepath.Join(repoPath, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(gitDir, "shallow.lock")
	attemptStarted := time.Now().Add(-time.Minute)

	if err := removeKilledAttemptShallowLock(repoPath, attemptStarted); err != nil {
		t.Fatalf("no lock: error = %v, want nil", err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := removeKilledAttemptShallowLock(repoPath, attemptStarted); err != nil {
		t.Fatalf("older lock: error = %v, want nil", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("a lock older than the attempt was removed: %v", err)
	}
	recent := attemptStarted.Add(10 * time.Second)
	if err := os.Chtimes(lock, recent, recent); err != nil {
		t.Fatal(err)
	}
	if err := removeKilledAttemptShallowLock(repoPath, attemptStarted); err != nil {
		t.Fatalf("attempt's lock: error = %v, want nil", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the killed attempt's lock is still there: stat error = %v", err)
	}
}
