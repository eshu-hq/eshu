// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !unix

package whispercpp

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcess(*exec.Cmd) {}

func applyAddressSpaceLimit(int, uint64) error { return nil }

func peakRSSBytes(*os.ProcessState) int64 { return 0 }

// checkPlatform fails closed: without process groups the engine cannot
// guarantee that cancellation kills every engine process.
func checkPlatform(Limits) error {
	return errors.New("whisper.cpp engine requires a Unix platform")
}
