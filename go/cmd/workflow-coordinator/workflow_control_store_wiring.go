// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// newWorkflowControlStore constructs the workflow coordinator's
// WorkflowControlStore and wires the shared meter-provider instruments and
// the per-component quota lookup (#8471) onto it. Extracted from run() so
// the wiring itself — not just the store construction — is directly
// unit-testable: a dropped Instruments assignment here would otherwise leave
// ReconcileWorkflowRuns correctly failing and logging a terminal reducer
// dead-letter block (#4459) while its documented
// eshu_dp_workflow_run_terminal_dead_letter_blocks_total metric silently
// never emits on the production path, with no test to catch the regression.
// A nil lookup keeps every claim on the plain statement, exactly as before;
// the lookup itself is built from the component manifest declarations with
// the operator override applied per field (component.Quota.Resolve).
func newWorkflowControlStore(database db.ExecQueryer, instruments *telemetry.Instruments, lookup workflow.QuotaLookup) *postgres.WorkflowControlStore {
	store := postgres.NewWorkflowControlStore(database)
	store.Instruments = instruments
	store.QuotaLookup = lookup
	return store
}
