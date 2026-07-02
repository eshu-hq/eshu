// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestNewWorkflowControlStoreWiresInstruments guards the codex review finding
// on PR #4518: run() must assign the shared meter-provider Instruments onto
// the WorkflowControlStore it constructs, or the #4459 terminal-dead-letter
// operator metric (eshu_dp_workflow_run_terminal_dead_letter_blocks_total)
// never emits on the real production path even though ReconcileWorkflowRuns
// still fails and logs the blocked run correctly. This test fails if a
// future edit drops the store.Instruments assignment inside
// newWorkflowControlStore.
func TestNewWorkflowControlStoreWiresInstruments(t *testing.T) {
	instruments := &telemetry.Instruments{}

	store := newWorkflowControlStore(postgres.SQLDB{}, instruments)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p) — the production wiring must assign it, not leave it nil", store.Instruments, instruments)
	}
}

// TestNewWorkflowControlStoreAllowsNilInstruments proves the wiring helper
// does not require Instruments to be non-nil: WorkflowControlStore treats a
// nil Instruments as a no-op for the dead-letter-block metric (never a
// panic), so a caller without a wired meter provider is unaffected.
func TestNewWorkflowControlStoreAllowsNilInstruments(t *testing.T) {
	store := newWorkflowControlStore(postgres.SQLDB{}, nil)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
}
