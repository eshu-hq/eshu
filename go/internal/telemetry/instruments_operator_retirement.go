// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerOperatorRepositoryRetirement registers the operator-driven
// repository retirement purge counter (#7766) on inst.
func registerOperatorRepositoryRetirement(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.OperatorRepositoryRetirementsPurged, err = meter.Int64Counter(
		"eshu_dp_repository_retirements_purged_total",
		metric.WithDescription("Operator-driven repository retirements the purge completed and tombstoned, by repository purged (#7766)"),
		metric.WithUnit("{retirement}"),
	); err != nil {
		return fmt.Errorf("register OperatorRepositoryRetirementsPurged counter: %w", err)
	}
	return nil
}
