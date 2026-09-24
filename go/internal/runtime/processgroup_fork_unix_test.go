// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	// forkCancelIterations is how many cancel-during-fork rounds run per test.
	// The kernel race is narrow (a few percent per round on a loaded
	// darwin/arm64 host), so the test loops instead of relying on one shot.
	forkCancelIterations = 60
	// forkCancelReturnBound bounds how long Wait may take after cancel. The
	// kill itself is near-immediate; the bound is measured from cancel, not
	// from process start, so host load spent spawning does not count.
	forkCancelReturnBound = 5 * time.Second
	// forkCancelSleep is how long each forked child would live if it survived
	// the group kill; it must dwarf forkCancelReturnBound.
	forkCancelSleep = "60"
)

// groupAlive reports whether any process is still a member of process group
// pgid. Wait has already reaped the group leader by the time callers ask, so a
// nil error means a surviving descendant.
func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

// TestNewProcessGroupCommandKillsChildForkedDuringCancel is the #7066
// regression. The group kill races a child the leader is forking at the
// instant of cancel: on darwin, killpg can return success yet miss that child,
// which is then orphaned to PID 1 and holds the command's stdout/stderr pipes
// open, so Cmd.Wait blocked for the orphan's whole lifetime (collector git's
// shutdown hung the same way behind a helper such as git-remote-https).
//
// Each round starts a shell that writes a marker and immediately forks several
// long sleeps, cancels the instant the marker appears (the shell is mid-fork),
// and requires that Wait returns within forkCancelReturnBound of cancel and
// that no process remains in the process group.
func TestNewProcessGroupCommandKillsChildForkedDuringCancel(t *testing.T) {
	dir := t.TempDir()
	iterations := forkCancelIterations
	if testing.Short() {
		iterations = 15
	}
	for i := 0; i < iterations; i++ {
		marker := filepath.Join(dir, "started-"+strconv.Itoa(i))
		script := ": > " + marker + "\n" +
			"sleep " + forkCancelSleep + " &\n" +
			"sleep " + forkCancelSleep + " &\n" +
			"sleep " + forkCancelSleep + " &\n" +
			"wait\n"

		ctx, cancel := context.WithCancel(context.Background())
		cmd := NewProcessGroupCommand(ctx, "sh", "-c", script)
		// Buffers (not nil) make exec create pipes; an orphan holding them is
		// exactly what stalls Wait.
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatalf("iteration %d: Start() error = %v", i, err)
		}
		pgid := cmd.Process.Pid
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		spawnDeadline := time.Now().Add(30 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(spawnDeadline) {
				cancel()
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				<-done
				t.Fatalf("iteration %d: marker %q never appeared; test setup is broken", i, marker)
			}
		}
		cancelAt := time.Now()
		cancel()

		select {
		case <-done:
		case <-time.After(forkCancelReturnBound):
			t.Errorf("iteration %d: Wait() still blocked %s after cancel; a child forked during the group kill survived and holds the pipes", i, time.Since(cancelAt))
			for groupAlive(pgid) {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				time.Sleep(5 * time.Millisecond)
			}
			<-done
		}
		if groupAlive(pgid) {
			t.Errorf("iteration %d: process group %d still has a live member after cancel", i, pgid)
			for groupAlive(pgid) {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
}
