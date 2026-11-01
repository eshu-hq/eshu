// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !linux

package tesseract

import (
	"os"
	"os/exec"
)

// configureProcess keeps the os/exec default of killing the direct child on
// cancellation. The reviewed runtime is Linux; other platforms run for
// development only.
func configureProcess(*exec.Cmd) {}

// limitAddressSpace is a no-op off Linux, where prlimit on another process
// is unavailable; the sandbox ceiling is the only memory bound there.
func limitAddressSpace(int, int64) error {
	return nil
}

// sampleHighWaterRSS reports zero where /proc VmHWM is unavailable.
func sampleHighWaterRSS(int) int64 {
	return 0
}

// terminationSignal reports no signal where wait status decoding is not
// wired.
func terminationSignal(*os.ProcessState) string {
	return ""
}
