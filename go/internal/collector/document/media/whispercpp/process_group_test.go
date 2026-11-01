// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build linux

package whispercpp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// firstHelperTimeout is the first wall-clock cap the timeout case tries.
const firstHelperTimeout = 500 * time.Millisecond

// TestTimeoutAndCancelKillTheWholeProcessGroup proves a helper the engine
// spawns dies with it. Killing only the engine PID would leave the sleeping
// helper alive and still holding the stdout pipe.
func TestTimeoutAndCancelKillTheWholeProcessGroup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(*testing.T) int
	}{
		{name: "timeout", run: helperPIDAfterTimeout},
		{name: "cancel", run: helperPIDAfterCancel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pid := tc.run(t)
			t.Cleanup(func() { killSurvivingHelper(pid) })
			deadline := time.Now().Add(5 * time.Second)
			for processAlive(pid) {
				if time.Now().After(deadline) {
					t.Fatalf("helper process %d survived the engine kill", pid)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

// helperPIDAfterTimeout runs the engine into its wall-clock cap and returns
// the helper PID. A cap can fire before the fake engine starts its helper,
// and that run proves nothing about the group kill, so the cap doubles until
// a run records a helper.
func helperPIDAfterTimeout(t *testing.T) int {
	t.Helper()
	for budget := firstHelperTimeout; budget <= helperStartupDeadline; budget *= 2 {
		engine := newFakeEngine(t, "grandchild", Limits{Timeout: budget})
		_, err := engine.Transcribe(context.Background(), wavMedia(t))
		if ClassOf(err) != ClassTimeout {
			t.Fatalf("ClassOf(%v) = %q, want %q", err, ClassOf(err), ClassTimeout)
		}
		if pid, err := helperPID(filepath.Dir(engine.cfg.ModelPath)); err == nil {
			return pid
		}
	}
	t.Fatalf("no run recorded a helper within a %s cap", helperStartupDeadline)
	return 0
}

// helperPIDAfterCancel cancels the run once the helper is recorded and
// returns its PID. Transcribe resamples the audio before exec, which is slow
// under -race, so a cancel on a fixed delay can land before the engine starts.
func helperPIDAfterCancel(t *testing.T) int {
	t.Helper()
	engine := newFakeEngine(t, "grandchild", Limits{})
	dir := filepath.Dir(engine.cfg.ModelPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		waitForHelper(ctx, dir)
	}()
	_, err := engine.Transcribe(ctx, wavMedia(t))
	if ClassOf(err) != ClassCanceled {
		t.Fatalf("ClassOf(%v) = %q, want %q", err, ClassOf(err), ClassCanceled)
	}
	pid, err := helperPID(dir)
	if err != nil {
		t.Fatalf("read helper PID: %v", err)
	}
	return pid
}

// waitForHelper returns once dir holds a valid helper PID, ctx ends, or
// helperStartupDeadline passes.
func waitForHelper(ctx context.Context, dir string) {
	ctx, cancel := context.WithTimeout(ctx, helperStartupDeadline)
	defer cancel()
	for ctx.Err() == nil {
		if _, err := helperPID(dir); err == nil {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// helperPID reads the PID the fake engine recorded in dir.
func helperPID(dir string) (int, error) {
	body, err := os.ReadFile(filepath.Join(dir, grandchildPIDFile))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || pid <= 1 {
		return 0, fmt.Errorf("helper PID %q is invalid", body)
	}
	return pid, nil
}

// killSurvivingHelper kills pid only while it is still the helper. The test
// usually proves the helper dead, and the kernel can then reuse its PID for
// an unrelated process.
func killSurvivingHelper(pid int) {
	if processAlive(pid) && isHelperProcess(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// isHelperProcess reports whether pid runs with the fake helper argument.
func isHelperProcess(pid int) bool {
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	return slices.Contains(args, grandchildArg)
}

// processAlive reports whether pid exists and is not a zombie awaiting
// reaping by init.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}
