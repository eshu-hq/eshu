// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix && !linux

package whispercpp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureProcess puts the engine in its own process group so cancellation
// kills the engine and any helper it spawned.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// applyAddressSpaceLimit is a no-op here; checkPlatform rejects a configured
// cap on non-Linux Unix before any run.
func applyAddressSpaceLimit(int, uint64) error { return nil }

// peakRSSBytes is not reported on non-Linux Unix, where ru_maxrss units
// differ by platform.
func peakRSSBytes(*os.ProcessState) int64 { return 0 }

func checkPlatform(limits Limits) error {
	if limits.MaxAddressSpaceBytes > 0 {
		return errors.New("whisper.cpp address-space cap requires Linux")
	}
	return nil
}
