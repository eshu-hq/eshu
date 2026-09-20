// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestGitSpawnsRouteThroughConstructor guards the group-kill invariant: every
// package-level git spawn must go through newGitCommand (a thin wrapper over
// runtime.NewProcessGroupCommand) so a cancelled sweep kills the whole helper
// tree instead of stranding remote-https grandchildren as PID 1 zombies. A
// bare exec.Command addition anywhere in this package fails here, not silently
// in production — no file is exempt. The gitsubmodule subpackage is out of
// scope (different directory and package; it routes its single local-only
// ls-tree site through runtime.NewProcessGroupCommand directly). Lines are cut
// at the first // so trailing comments naming the constructor do not trip the
// guard; a call hiding behind a URL string literal on the same line would be
// missed — keep spawn lines simple.
func TestGitSpawnsRouteThroughConstructor(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; test setup is broken")
	}
	entries, err := os.ReadDir(filepath.Dir(thisFile))
	if err != nil {
		t.Fatalf("ReadDir(package) error = %v", err)
	}
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), name))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "//"); idx >= 0 {
				line = line[:idx]
			}
			// Both constructors: exec.Command and exec.CommandContext.
			if strings.Contains(line, "exec.Command(") ||
				strings.Contains(line, "exec.CommandContext(") {
				violations = append(violations, name+":"+strconv.Itoa(i+1))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("raw exec.Command at %v; route git spawns through newGitCommand", violations)
	}
}

// TestNewGitCommandUsesProcessGroup ensures every git spawn routes through the
// group-kill constructor so a cancelled sweep never strands remote-https
// helpers.
func TestNewGitCommandUsesProcessGroup(t *testing.T) {
	cmd := newGitCommand(context.Background(), "rev-parse", "HEAD")
	if cmd.Path == "" && len(cmd.Args) == 0 {
		t.Fatal("newGitCommand returned an empty command")
	}
	if cmd.Args[0] == "" || !strings.HasSuffix(cmd.Args[0], "git") {
		t.Fatalf("newGitCommand argv[0] = %q, want the git binary", cmd.Args[0])
	}
	if runtime.GOOS != "windows" && cmd.Cancel == nil {
		t.Fatal("newGitCommand did not set group-kill Cancel on unix")
	}
}

// countZombieChildren counts this process's zombie children via /proc.
// Linux-only callers; it returns -1 when /proc is unreadable.
func countZombieChildren() int {
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return -1
	}
	n := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(data)
		paren := strings.LastIndex(s, ")")
		if paren < 0 {
			continue
		}
		fields := strings.Fields(s[paren+2:])
		if len(fields) > 1 && fields[0] == "Z" && fields[1] == strconv.Itoa(self) {
			n++
		}
	}
	return n
}

// runGitViaConstructor is a test helper that runs one git command through the
// production constructor and fails the test on error.
func runGitViaConstructor(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	cmd := newGitCommand(ctx, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
}

// TestFetchChurnZombiesDrainedByReaper is the live-churn companion to the
// orphan regression: real `git fetch` operations over HTTP fork the
// remote-https helper, and the helper's teardown intermittently outlives the
// parent's exit so it reparents here as an unreaped zombie (the ops-qa pids
// exhaustion, issue #6873). Phase 1 churns fetches with no reaper running and
// requires zombies to accumulate, proving the test exercises the real leak;
// phase 2 drains them with the production reaper and requires zero left.
func TestFetchChurnZombiesDrainedByReaper(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("orphan adoption needs Linux subreaper semantics")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	if err := runtimecfg.EnableChildSubreaper(nil); err != nil {
		t.Skipf("PR_SET_CHILD_SUBREAPER unavailable: %v", err)
	}
	ctx := context.Background()
	root := t.TempDir()

	// Fixture source repo served over dumb HTTP (plain static files: no git
	// server binary needed, and the http:// URL still forks the real
	// remote-https helper on the client).
	src := filepath.Join(root, "src")
	git := func(dir string, args ...string) {
		full := append([]string{"-c", "user.email=churn@test", "-c", "user.name=churn", "-c", "init.defaultBranch=main"}, args...)
		cmd := exec.Command("git", append([]string{"-C", dir}, full...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	git(root, "init", "src")
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(src, "add", ".")
	git(src, "commit", "-m", "init")
	git(src, "update-server-info")
	srv := httptest.NewServer(http.FileServer(http.Dir(root)))
	defer srv.Close()

	work := filepath.Join(root, "work")
	runGitViaConstructor(t, ctx, root, "clone", "-q", srv.URL+"/src/.git", work)

	// Phase 1: churn fetches through the production constructor with no
	// reaper running until at least one zombie is adopted (bounded: the
	// teardown race is intermittent locally, ~1 per 10 fetches over
	// loopback, near-certain over up to 100).
	const maxFetches = 100
	accumulated := 0
	for i := 0; i < maxFetches; i++ {
		runGitViaConstructor(t, ctx, work, "fetch", "-q", "origin")
		if n := countZombieChildren(); n > 0 {
			accumulated = n
			break
		}
	}
	t.Logf("fetch churn adopted %d zombie(s)", accumulated)
	if accumulated == 0 {
		t.Fatalf("no zombies adopted over %d fetches; test does not exercise the leak", maxFetches)
	}

	// Phase 2: the production reaper drains everything it observed.
	reaper := runtimecfg.NewOrphanReaper(0, time.Second, nil)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		reaper.ScanOnce()
		if n := countZombieChildren(); n == 0 {
			t.Logf("reaper drained %d adopted zombie(s) to zero", accumulated)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("reaper left %d zombies; fix does not hold under churn", countZombieChildren())
}
