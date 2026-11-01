// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build linux

package whispercpp

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// configureProcess puts the engine in its own process group so cancellation
// kills the engine and any helper it spawned.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// applyAddressSpaceLimit sets RLIMIT_AS on the started engine. The engine runs
// uncapped between exec and this call, and the length of that window was not
// measured; the container memory limit stays the primary cap.
func applyAddressSpaceLimit(pid int, limit uint64) error {
	if limit == 0 {
		return nil
	}
	return unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{Cur: limit, Max: limit}, nil)
}

func checkPlatform(Limits) error { return nil }

// peakRSSBytes reports the exited engine's peak resident set size. Linux
// reports ru_maxrss in KiB.
func peakRSSBytes(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage == nil {
		return 0
	}
	return usage.Maxrss * 1024
}
