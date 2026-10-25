// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerSecretLinesRepairInstruments registers the reducer's hardcoded-secret
// side table repair counter (#8833 ruling-3) on inst.
func registerSecretLinesRepairInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SecretLinesRepairRuns, err = meter.Int64Counter(
		"eshu_dp_secret_lines_repair_runs_total",
		metric.WithDescription("Reducer polls of the hardcoded-secret side table repair by outcome (skipped_ready, skipped_locked, published, superseded, cancelled, failed). While the state is not ready, reads use the legacy scan; repeated failed means the repair cannot publish."),
	); err != nil {
		return fmt.Errorf("register SecretLinesRepairRuns counter: %w", err)
	}
	return nil
}
