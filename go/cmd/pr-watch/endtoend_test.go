// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fakeGHEnv = "PR_WATCH_FAKE_GH_DIR"

// TestMain lets the test binary stand in for the gh executable: when
// PR_WATCH_FAKE_GH_DIR is set, the process is a seeded fake gh, not a test run.
func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeGHEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeGH serves seeded GraphQL polls and job logs from dir. It refuses any call
// that does not carry the pinned token, which proves the watcher pins the
// account on the real process boundary, not only in an injected Exec.
func fakeGH(dir string, args []string) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, format+"\n", a...)
		return 1
	}
	if len(args) >= 4 && args[0] == "auth" && args[1] == "token" {
		if os.Getenv("GH_TOKEN") != "" {
			return fail("token lookup must not inherit GH_TOKEN")
		}
		fmt.Println("tok-" + args[3])
		return 0
	}
	if os.Getenv("GH_TOKEN") != "tok-acct" {
		return fail("HTTP 401: Bad credentials (GH_TOKEN=%q)", os.Getenv("GH_TOKEN"))
	}
	switch {
	case len(args) >= 2 && args[0] == "api" && args[1] == "graphql":
		num := argValue(args, "number")
		files, _ := filepath.Glob(filepath.Join(dir, "pr-"+num, "poll-*.json"))
		sort.Strings(files)
		if len(files) == 0 {
			return fail("no seeded polls for pr %s", num)
		}
		counter := filepath.Join(dir, "reads-"+num)
		raw, _ := os.ReadFile(counter)
		i, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		_ = os.WriteFile(counter, []byte(strconv.Itoa(i+1)), 0o600)
		if i >= len(files) {
			i = len(files) - 1
		}
		body, err := os.ReadFile(files[i])
		if err != nil {
			return fail("%v", err)
		}
		_, _ = os.Stdout.Write(body)
		return 0
	case len(args) >= 3 && args[0] == "api" && args[1] == "--allow-escape-sequences":
		id := args[2][strings.LastIndex(args[2], "jobs/")+len("jobs/"):]
		body, err := os.ReadFile(filepath.Join(dir, "log-"+strings.TrimSuffix(id, "/logs")+".txt"))
		if err != nil {
			return fail("gh: Not Found (HTTP 404)")
		}
		_, _ = os.Stdout.Write(body)
		return 0
	}
	return fail("unexpected fake gh call %v", args)
}

// gitRun runs git in dir with a fixed identity and no signing.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// conflictingRepo builds an origin and a clone where main and the pull head both
// rewrote CHANGELOG.md, and returns the clone path with the two SHAs.
func conflictingRepo(t *testing.T) (clone, headSHA, baseSHA string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	clone = filepath.Join(root, "clone")
	gitRun(t, root, "init", "--bare", origin)
	gitRun(t, root, "clone", origin, clone)
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(clone, "CHANGELOG.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("line\n")
	gitRun(t, clone, "add", ".")
	gitRun(t, clone, "commit", "-m", "init")
	gitRun(t, clone, "branch", "-M", "main")
	gitRun(t, clone, "push", "origin", "main")
	gitRun(t, clone, "checkout", "-b", "feature")
	write("feature change\n")
	gitRun(t, clone, "commit", "-am", "feature")
	headSHA = gitRun(t, clone, "rev-parse", "HEAD")
	gitRun(t, clone, "push", "origin", "feature:refs/pull/7/head")
	gitRun(t, clone, "checkout", "main")
	write("main change\n")
	gitRun(t, clone, "commit", "-am", "main moves")
	baseSHA = gitRun(t, clone, "rev-parse", "HEAD")
	gitRun(t, clone, "push", "origin", "main")
	return clone, headSHA, baseSHA
}

func TestEndToEndAgainstFakeGHAndRealGit(t *testing.T) {
	clone, head, base := conflictingRepo(t)
	scenario := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scenario, "pr-7"), 0o700); err != nil {
		t.Fatal(err)
	}
	seed := func(name string, f fx) {
		if err := os.WriteFile(filepath.Join(scenario, "pr-7", name), f.wire(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p1 := openPR(7, head)
	p1.headRef, p1.baseOID = "feature", base
	p1.mergeable, p1.mergeState = "CONFLICTING", "DIRTY"
	p1.rollup, p1.total = "PENDING", 1
	p1.checks = []ck{{run: 11, name: "go-core", status: "IN_PROGRESS", url: jobDetails}}
	p2 := p1
	p2.rollup = "FAILURE"
	p2.checks = []ck{{run: 11, name: "go-core", status: "COMPLETED", result: "FAILURE", url: jobDetails}}
	p2.threads = []thr{{id: "T1", path: "a.go", line: 9, author: "codex", body: "bug"}}
	p4 := p2
	p4.state, p4.mergedAt, p4.mergeOID = "MERGED", "2026-10-25T13:00:00Z", "mergecommit"
	seed("poll-1.json", p1)
	seed("poll-2.json", p2)
	seed("poll-3.json", p2)
	seed("poll-4.json", p4)
	if err := os.WriteFile(filepath.Join(scenario, "log-777.txt"), []byte("go test\n--- FAIL: TestBoom\n##[error]exit 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeGHEnv, scenario)
	t.Setenv("GH_TOKEN", "") // the parent environment must not leak a token to children

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	code := run(ctx, []string{
		"-gh", self, "-dir", clone, "-account", "acct", "-repo", "eshu-hq/eshu",
		"-interval", "10ms", "-log-lines", "2", "7",
	}, &stdout, &stderr, os.Environ(), t.TempDir(), osExec)
	if code != 0 {
		t.Fatalf("run exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	wantKinds(t, lines, "WATCH", "BASELINE", "CONFLICT", "NEW-THREAD", "CI-FAIL", "MERGED", "WATCH")
	conflict := parseEvent(t, lines[2])
	if conflict.kv["test_merge"] != "conflict" || conflict.kv["files"] != "CHANGELOG.md" || conflict.kv["recipe"] != "4a" {
		t.Fatalf("real git test merge misreported: %s", lines[2])
	}
	if tail := parseEvent(t, lines[4]).kv["tail"]; tail != "--- FAIL: TestBoom | ##[error]exit 1" {
		t.Fatalf("log tail = %q", tail)
	}
	if last := parseEvent(t, lines[len(lines)-1]); last.kv["state"] != "done" {
		t.Fatalf("watcher must end with a done line, got %s", lines[len(lines)-1])
	}
}

func TestRunRejectsBadUsageAndUnknownAccount(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"-account", "a"}, &stdout, &stderr, nil, "/h", osExec); code != 2 {
		t.Fatalf("no PRs: exit %d, want 2", code)
	}
	if code := run(context.Background(), []string{"-account", "a", "abc"}, &stdout, &stderr, nil, "/h", osExec); code != 2 {
		t.Fatalf("bad PR: exit %d, want 2", code)
	}
	stdout.Reset()
	code := run(context.Background(), []string{"-dir", t.TempDir(), "5"}, &stdout, &stderr, nil, "/nowhere", osExec)
	if code != 1 || !strings.Contains(stdout.String(), "WATCH state=exit") {
		t.Fatalf("unknown account must exit 1 with a WATCH exit line, got %d %q", code, stdout.String())
	}
}

func TestRunStopsOnInterruptWithAWatchLine(t *testing.T) {
	w := newWorld()
	w.seed(3, openPR(3, "aaaa1111"))
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-account", "acct", "-interval", "20ms", "3"}, &stdout, &stderr, nil, "/h", w.Exec)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("interrupt exit = %d, stderr %s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not stop after cancel")
	}
	if !strings.Contains(stdout.String(), "WATCH state=stopped reason=interrupted") {
		t.Fatalf("interrupt must leave a WATCH line, got %q", stdout.String())
	}
}
