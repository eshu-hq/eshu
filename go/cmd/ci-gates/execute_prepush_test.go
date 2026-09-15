// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// The DEFER-CI print path: executeGates must never fold a deferred gate into
// the generic SKIP line, and must never run its command.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestExecuteGatesDeferredGatePrintsDeferCIAndDoesNotRun proves a Selection
// with Deferred=true (what cigates.FilterPrePush produces for a triggered,
// pre_push:deferred gate) prints "DEFER-CI <id>: <reason>", never runs its
// local.command, and does not fail the run -- deferral moves enforcement to
// CI, it is not a failure.
func TestExecuteGatesDeferredGatePrintsDeferCIAndDoesNotRun(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := cigates.Selection{
		Selected: false,
		Deferred: true,
		Reason:   "median 70s of the pre-push run; still runs in make pre-pr and CI",
		Gate: cigates.Gate{
			ID:       "slow-gate",
			Blocking: true,
			Local:    &cigates.Local{Command: "printf 'ran\\n' >> trace.log", PrePushDeferred: true},
		},
	}

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err != nil {
		t.Fatalf("executeGates() error = %v, want nil (a deferral is not a failure)", err)
	}
	if !strings.Contains(output.String(), "DEFER-CI slow-gate: median 70s") {
		t.Fatalf("executeGates() output did not print DEFER-CI:\n%s", output.String())
	}
	if strings.Contains(output.String(), "SKIP     slow-gate") {
		t.Fatalf("executeGates() printed a generic SKIP for a deferred gate:\n%s", output.String())
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "trace.log")); err == nil {
		t.Fatal("executeGates() ran the deferred gate's command; it must not")
	}
}
