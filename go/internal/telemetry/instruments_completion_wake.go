// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Completion-wake outcomes (#8294), recorded on the existing outcome
// dimension. The value set is closed.
const (
	// CompletionWakeOutcomeReopened means the wake transaction reopened at
	// least one succeeded item or flagged at least one in-flight item for
	// replay after its ACK.
	CompletionWakeOutcomeReopened = "reopened"
	// CompletionWakeOutcomeNoop means the wake transaction ran and matched
	// nobody: every overlapping item's recorded input still holds.
	CompletionWakeOutcomeNoop = "noop"
	// CompletionWakeOutcomeError means the wake transaction failed and
	// rolled back; the resolution item retries and re-runs the wake.
	CompletionWakeOutcomeError = "error"
)

// registerCompletionWakeInstruments registers the #8294 completion-wake
// counter on inst. One increment per wake transaction, never per row: the
// only label is the closed outcome set, with no scope or work-item id. The
// reopened row count rides eshu_dp_correlation_reopened_total alongside the
// maintenance reopen.
func registerCompletionWakeInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerCompletionWake, err = meter.Int64Counter(
		"eshu_dp_reducer_completion_wake_total",
		metric.WithDescription("Completion-driven deployable_unit_correlation wake transactions after resolved-relationship resolution by outcome (reopened, noop, error) (#8294)"),
	); err != nil {
		return fmt.Errorf("register ReducerCompletionWake counter: %w", err)
	}
	return nil
}
