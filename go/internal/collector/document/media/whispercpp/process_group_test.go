// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build linux

package whispercpp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestTimeoutAndCancelKillTheWholeProcessGroup proves a helper the engine
// spawns dies with it. Killing only the engine PID would leave the sleeping
// helper alive and still holding the stdout pipe.
func TestTimeoutAndCancelKillTheWholeProcessGroup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		run   func(*Engine) error
		class ErrorClass
	}{
		{
			name:  "timeout",
			run:   func(e *Engine) error { _, err := e.Transcribe(context.Background(), wavMedia(t)); return err },
			class: ClassTimeout,
		},
		{
			name: "cancel",
			run: func(e *Engine) error {
				ctx, cancel := context.WithCancel(context.Background())
				time.AfterFunc(500*time.Millisecond, cancel)
				_, err := e.Transcribe(ctx, wavMedia(t))
				return err
			},
			class: ClassCanceled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			limits := Limits{}
			if tc.class == ClassTimeout {
				limits.Timeout = 500 * time.Millisecond
			}
			engine := newFakeEngine(t, "grandchild", limits)
			if err := tc.run(engine); ClassOf(err) != tc.class {
				t.Fatalf("ClassOf(%v) = %q, want %q", err, ClassOf(err), tc.class)
			}
			pid := readGrandchildPID(t, filepath.Dir(engine.cfg.ModelPath))
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
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

func readGrandchildPID(t *testing.T, dir string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, grandchildPIDFile))
	if err != nil {
		t.Fatalf("read helper PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || pid <= 1 {
		t.Fatalf("helper PID %q is invalid", body)
	}
	return pid
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
