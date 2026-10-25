// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"regexp"
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

// repairerCall matches the lines.NewRepairer( call in run and captures its
// first two arguments: the shared database and the lock Conner.
var repairerCall = regexp.MustCompile(`lines\.NewRepairer\(\s*([^,]+(?:\{[^}]*\})?),\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,`)

// TestRunGivesTheRepairerItsDedicatedLockPool pins the Conner argument that the
// secret/lines pool-of-one test cannot reach (#8833 PR2b review F9): run must
// build the Repairer with the pool opened by openSecretLinesLockPool, never the
// shared db. With the shared pool the lock pins the only connection of a
// ESHU_POSTGRES_MAX_OPEN_CONNS=1 reducer and Finalize waits on it forever.
func TestRunGivesTheRepairerItsDedicatedLockPool(t *testing.T) {
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
	match := repairerCall.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("run has no lines.NewRepairer(<database>, <lock pool>, ...) call")
	}
	lockArg := match[2]
	if lockArg == "db" || strings.Contains(match[1], lockArg) {
		t.Fatalf("lines.NewRepairer's lock Conner is %q, the shared pool: the lock would pin the only connection of a pool of one", lockArg)
	}
	definition := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(lockArg) + `,\s*err\s*:?=\s*openSecretLinesLockPool\(`)
	if !definition.MatchString(body) {
		t.Fatalf("the Repairer's lock Conner %q is not the result of openSecretLinesLockPool(", lockArg)
	}
}
