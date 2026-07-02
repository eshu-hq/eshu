// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newWorkflowControlStore constructs the workflow coordinator's
// WorkflowControlStore and wires the shared meter-provider instruments onto
// it. Extracted from run() so the wiring itself — not just the store
// construction — is directly unit-testable: a dropped Instruments assignment
// here would otherwise leave ReconcileWorkflowRuns correctly failing and
// logging a terminal reducer dead-letter block (#4459) while its documented
// eshu_dp_workflow_run_terminal_dead_letter_blocks_total metric silently
// never emits on the production path, with no test to catch the regression.
func newWorkflowControlStore(db postgres.ExecQueryer, instruments *telemetry.Instruments) *postgres.WorkflowControlStore {
	store := postgres.NewWorkflowControlStore(db)
	store.Instruments = instruments
	return store
}
