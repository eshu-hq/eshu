// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// mirrorRepoRoot resolves the enclosing eshu checkout root from this test
// file's own path, so the mirror below always executes the working tree it
// runs in (go/cmd/golden-corpus-gate/static_mirror_test.go is four levels
// below the root).
func mirrorRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot locate the checkout root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

// TestGoldenCorpusMirrorContract is the converted golden-corpus-mirror gate
// (#10988, part of #9315): it executes the golden-corpus gate's hermetic
// self-test suite — no Docker, no ports — under `go test`, so test.yml and
// the re-pointed static-mirror cell share one runner. The script stays the
// assertion body; the Go test is the invocation contract. A drifted verifier,
// fixture, or ledger fails here exactly as it failed the old gate cell.
func TestGoldenCorpusMirrorContract(t *testing.T) {
	root := mirrorRepoRoot(t)
	script := filepath.Join(root, "scripts", "test-verify-golden-corpus-gate.sh")

	// Fail closed on a hang: the suite normally finishes in about ninety
	// seconds, well inside the static-mirror cell's timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("hermetic golden-corpus self-test failed: %v\n%s", err, out.String())
	}
}
