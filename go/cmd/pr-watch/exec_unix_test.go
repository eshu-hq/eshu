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
// only the direct child leaves the grandchild running, holding the pipes.
func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidfile := t.TempDir() + "/grandchild.pid"
	t.Setenv(hangPidfileEnv, pidfile)
	cfg := Config{GH: self, Git: "git", GHTimeout: 400 * time.Millisecond}
	exec := withTimeouts(osExec, cfg)

	start := time.Now()
	_, runErr := exec(context.Background(), Command{Name: self, Args: []string{"api", "graphql"}, Env: os.Environ()})
	if runErr == nil || !strings.Contains(runErr.Error(), "timed out") {
		t.Fatalf("want a timeout error, got %v", runErr)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("the timeout took %s to return", took)
	}
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("the helper never recorded its grandchild: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // never leave the stray sleep behind
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d is still running after the timeout; the process group was not killed", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
