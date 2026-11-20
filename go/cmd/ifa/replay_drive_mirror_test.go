// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestIfaReplayDriveMirrorContract is the converted ifa-replay-drive gate
// (#10988, part of #9315): it executes the Ifá replay-drive structural mirror
// — bash version guard, drive/drain contract pins on
// scripts/verify-ifa-replay-drive.sh — under `go test`, so test.yml and the
// re-pointed static-contract cell share one runner. The script stays the
// assertion body; the Go test is the invocation contract. A refactor that
// drops the guard, the pid capture, or a drive/drain pin fails here exactly
// as it failed the old gate cell.
func TestIfaReplayDriveMirrorContract(t *testing.T) {
	root := repoRootDir(t)
	script := filepath.Join(root, "scripts", "test-verify-ifa-replay-drive.sh")

	// Fail closed on a hang: the suite normally finishes in under a second.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("ifa replay-drive mirror failed: %v\n%s", err, out.String())
	}
}
