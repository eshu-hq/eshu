// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const hangPidfileEnv = "PR_WATCH_FAKE_HANG_PIDFILE"

func init() {
	helperModes = append(helperModes, hangWithGrandchild)
}

// hangWithGrandchild is the test binary playing a gh that hangs: it starts a
// long-lived grandchild, records the grandchild's pid, and blocks.
func hangWithGrandchild() (int, bool) {
	pidfile := os.Getenv(hangPidfileEnv)
	if pidfile == "" {
		return 0, false
	}
	grandchild := exec.Command("sleep", "300")
	if err := grandchild.Start(); err != nil {
		return 1, true
	}
	// Reap it, so a grandchild that exits early reads as gone, not as a zombie
	// that kill(pid, 0) still reports as alive.
	go func() { _ = grandchild.Wait() }()
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(grandchild.Process.Pid)), 0o600); err != nil {
		return 1, true
	}
	time.Sleep(5 * time.Minute)
	return 0, true
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// TestTimeoutKillsTheWholeProcessGroup runs a real child that forks a real
// grandchild, lets the deadline fire, and requires both to be gone. Killing
// only the direct child leaves the grandchild running, holding the pipes. It
// also requires the grandchild to be alive while the call is still in flight,
// so a grandchild that died early cannot pass for a killed one.
func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidfile := t.TempDir() + "/grandchild.pid"
	t.Setenv(hangPidfileEnv, pidfile)
	// The deadline is long enough for the re-executed test binary to start,
	// fork its grandchild, and record the pid before the deadline fires.
	const limit = 3 * time.Second
	cfg := Config{GH: self, Git: "git", GHTimeout: limit}
	run := withTimeouts(osExec, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		_, runErr := run(ctx, Command{Name: self, Args: []string{"api", "graphql"}, Env: os.Environ()})
		done <- runErr
	}()
	t.Cleanup(func() {
		cancel() // an early failure must not leave the helper blocked for minutes
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
		}
	})

	pid := waitForPid(t, pidfile, done)
	t.Cleanup(func() {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // never leave the stray sleep behind
		}
	})
	// Look again just before the deadline: a grandchild that died early must
	// not be mistaken for one the timeout killed.
	time.Sleep(time.Until(start.Add(limit - 700*time.Millisecond)))
	select {
	case runErr := <-done:
		t.Fatalf("the call returned (%v) before the deadline check; the machine is too slow for this test", runErr)
	default:
	}
	if !processAlive(pid) {
		t.Fatalf("grandchild %d is already gone just before the deadline; the test cannot show the timeout killed it", pid)
	}

	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "timed out") {
			t.Fatalf("want a timeout error, got %v", runErr)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the call did not return after its deadline")
	}
	if took := time.Since(start); took > 9*time.Second {
		t.Fatalf("the timeout took %s to return", took)
	}
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d is still running after the timeout; the process group was not killed", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForPid reads the grandchild pid the helper records. It fails if the call
// returns first, which means the helper never got as far as writing it.
func waitForPid(t *testing.T, pidfile string, done chan error) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidfile); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil {
				return pid
			}
		}
		select {
		case runErr := <-done:
			t.Fatalf("the helper never recorded its grandchild (call returned: %v)", runErr)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("the helper never recorded its grandchild")
	return 0
}
