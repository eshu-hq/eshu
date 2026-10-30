// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
// nil error means a surviving descendant. Zombies answer signal 0 too, so the
// raw probe also reports a fully-dead group as alive until PID 1 reaps it;
// groupHasLiveMember is the check that tells the two apart.
func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

// procState reports the single-letter state in /proc/pid/stat ("Z" for
// zombie, "R"/"S" while running), or "" when the pid is gone or unreadable.
func procState(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(data)
	paren := strings.LastIndex(s, ")")
	if paren < 0 {
		return ""
	}
	fields := strings.Fields(s[paren+2:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// groupHasLiveMember reports whether pgid still has a member that is not a
// zombie. On Linux it scans /proc for a group member outside state Z,
// following the processAlive pattern in processgroup_test.go; where /proc is
// unavailable the raw probe is the only signal and every member counts.
func groupHasLiveMember(pgid int) bool {
	if runtime.GOOS == "linux" {
		want := strconv.Itoa(pgid)
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return groupAlive(pgid)
		}
		for _, e := range entries {
			if _, err := strconv.Atoi(e.Name()); err != nil {
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
			if len(fields) > 2 && fields[2] == want && fields[0] != "Z" {
				return true
			}
		}
		return false
	}
	return groupAlive(pgid)
}

// waitForGroupEmpty polls until pgid has no live (non-zombie) member or the
// timeout passes. Orphaned grandchildren reparent to PID 1 on cancel and read
// as group members until init reaps them; under load that reap lags the kill,
// so an instant probe flakes (#9560). A genuinely missed child holds the
// command's pipes and already trips the Wait bound above, so waiting here
// cannot mask a real survivor.
func waitForGroupEmpty(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !groupHasLiveMember(pgid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
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
			time.Sleep(time.Millisecond)
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
		if !waitForGroupEmpty(pgid, 10*time.Second) {
			t.Errorf("iteration %d: process group %d still has a live member after cancel", i, pgid)
			for groupAlive(pgid) {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
}

// TestGroupProbeIgnoresZombieDescendant is the #9560 regression. A SIGKILLed
// group member stays visible to kill(-pgid, 0) until PID 1 reaps it: a zombie
// answers signal 0, so the raw probe reports a fully-dead group as alive and
// the post-cancel check flaked under floor load when the reap lagged the
// kill. The group check must therefore ignore zombies.
func TestGroupProbeIgnoresZombieDescendant(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("zombie observation needs Linux /proc")
	}
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pgid := cmd.Process.Pid
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	// Only this test can reap its own child, so once /proc reports Z the
	// process is guaranteed to stay an unreaped zombie until cmd.Wait below.
	deadline := time.Now().Add(10 * time.Second)
	for procState(pgid) != "Z" {
		if time.Now().After(deadline) {
			_ = cmd.Wait()
			t.Fatal("SIGKILLed child never reached zombie state; test setup is broken")
		}
		time.Sleep(time.Millisecond)
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		_ = cmd.Wait()
		t.Fatalf("raw probe already reports the zombie group empty; this test cannot demonstrate the flaw it pins: %v", err)
	}
	if groupHasLiveMember(pgid) {
		_ = cmd.Wait()
		t.Fatal("groupHasLiveMember reports a live member for a group whose only member is a zombie")
	}
	_ = cmd.Wait()
}

// TestNewProcessGroupCommandKeepsSuccessWhenDescendantHoldsPipes pins that a
// command which exits 0 is not turned into a failure because a descendant
// still holds its stdout/stderr pipes. exec.Cmd.WaitDelay would return
// exec.ErrWaitDelay in that case (Go reports it only when the exit status was
// zero); collector git callers treat any error as a failed clone and delete the
// checkout, so the constructor must leave WaitDelay unset and let Wait block
// until the pipe holder closes them.
func TestNewProcessGroupCommandKeepsSuccessWhenDescendantHoldsPipes(t *testing.T) {
	// The background sleep outlives the shell and keeps stdout open for longer
	// than any WaitDelay this package could plausibly choose while staying short
	// enough for a unit test.
	cmd := NewProcessGroupCommand(context.Background(), "sh", "-c", "echo out; sleep 4 & exit 0")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrWaitDelay) {
			t.Fatalf("Output() error = %v; a zero exit must not become a failure because a descendant holds the pipes", err)
		}
		t.Fatalf("Output() error = %v, want nil", err)
	}
	if got, want := string(out), "out\n"; got != want {
		t.Fatalf("Output() = %q, want %q", got, want)
	}
}
