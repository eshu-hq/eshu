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

// TestReplayCoverageMirrorContract is the converted
// replay-coverage-static-mirror gate (#10988, part of #9315): it executes the
// replay-coverage gate's static contract self-test — verifier, workflow, and
// registry agreement, no Go build, no Docker — under `go test`, so test.yml
// and the replay-coverage-gate row's test_command share one runner. The
// script stays the assertion body; the Go test is the invocation contract. A
// drifted verifier, workflow, manifest, or registry entry fails here exactly
// as it failed the old static-mirror cell.
func TestReplayCoverageMirrorContract(t *testing.T) {
	root := repoRootFromTest(t)
	script := filepath.Join(root, "scripts", "test-verify-replay-coverage-gate.sh")

	// Fail closed on a hang: the suite normally finishes in under a second.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("replay-coverage static contract self-test failed: %v\n%s", err, out.String())
	}
}
