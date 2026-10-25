// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunWiresTheSecretLinesRepairIntoTheShutdownOrder pins the wiring that the
// helper-level tests cannot reach (#8833 PR2b review F3): run must start the
// repair, and its shutdown defer must, after stop(), wait for the repair
// before closing the dedicated lock pool, and only then reach the waits and
// the database close that follow. A repair that outlives the pool it pins a
// connection from, or that is never started, fails here.
func TestRunWiresTheSecretLinesRepairIntoTheShutdownOrder(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "run.go"))
	if err != nil {
		t.Fatal(err)
	}
	body, ok := extractFuncBody(string(source), "func run(")
	if !ok {
		t.Fatal("func run( not found in run.go")
	}
	if !strings.Contains(body, "startSecretLinesRepair(") {
		t.Fatal("run does not call startSecretLinesRepair(: the secret-lines repair is never started")
	}
	const deferHead = "defer func() {\n\t\tstop()\n"
	start := strings.Index(body, deferHead)
	if start == -1 {
		t.Fatalf("run has no shutdown defer that begins with stop():\n%s", deferHead)
	}
	block := body[start:]
	if end := strings.Index(block, "\n\t}()"); end != -1 {
		block = block[:end]
	}
	wait := strings.Index(block, "waitSecretLinesRepair()")
	closePool := strings.Index(block, "closeSecretLinesLockPool(")
	if wait == -1 {
		t.Fatalf("the shutdown defer does not call waitSecretLinesRepair() after stop():\n%s", block)
	}
	if closePool == -1 || closePool < wait {
		t.Fatalf("the shutdown defer must close the dedicated lock pool (closeSecretLinesLockPool) after waitSecretLinesRepair():\n%s", block)
	}
}
