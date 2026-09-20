// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// processAlive reports whether pid is a live, non-zombie process.
func processAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	paren := strings.LastIndex(s, ")")
	if paren < 0 {
		return false
	}
	fields := strings.Fields(s[paren+2:])
	return len(fields) > 0 && fields[0] != "Z"
}

// childPidsOf returns the pids whose parent is ppid, read from /proc.
func childPidsOf(ppid int) []int {
	if runtime.GOOS == "windows" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	want := strconv.Itoa(ppid)
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pid == ppid {
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
		if len(fields) > 1 && fields[1] == want {
			out = append(out, pid)
		}
	}
	return out
}

// TestNewProcessGroupCommandKillsTreeOnCancel proves the cancel path of the
// orphan fix: helper grandchildren that survive a bare SIGKILL of the direct
// child (collector git's remote-https wrapper strands this way on every
// cancelled sweep) must die with the context when the whole process group is
// killed.
func TestNewProcessGroupCommandKillsTreeOnCancel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("grandchild observation needs Linux /proc")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// sh backgrounds a sleep grandchild and waits: cancelling must kill both.
	cmd := NewProcessGroupCommand(ctx, "sh", "-c", "sleep 30 & wait")
	if cmd.Cancel == nil {
		t.Fatal("NewProcessGroupCommand did not set Cmd.Cancel; grandchildren would survive ctx cancel")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	self := cmd.Process.Pid
	// Wait for the grandchild to exist.
	var tree []int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tree = childPidsOf(self)
		if len(tree) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(tree) == 0 {
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for grandchild; test setup is broken")
	}
	cancel()
	_ = cmd.Wait()
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		stillAlive := false
		for _, pid := range append([]int{self}, tree...) {
			if processAlive(pid) {
				stillAlive = true
				break
			}
		}
		// Re-check for grandchildren forked between snapshots.
		for _, pid := range childPidsOf(self) {
			if processAlive(pid) {
				stillAlive = true
				break
			}
		}
		if !stillAlive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process tree survived cancel: root %d children %v", self, tree)
}
