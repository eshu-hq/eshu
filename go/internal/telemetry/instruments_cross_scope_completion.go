// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCrossScopeCompletionInstruments registers the #10972 skipped-marker
// counter on inst. One add per committed completion fan-out, by the producer
// domain on the existing domain dimension (a closed set from the crossscope
// dependency catalog), never a scope or work-item id.
func registerCrossScopeCompletionInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CrossScopeCompletionMarkersSkipped, err = meter.Int64Counter(
		"eshu_dp_cross_scope_completion_markers_skipped_total",
		metric.WithDescription("Manifest-marker supply_chain_impact consumers a cross-scope completion fan-out left unscheduled and unflagged, by producer domain (#10972)"),
	); err != nil {
		return fmt.Errorf("register CrossScopeCompletionMarkersSkipped counter: %w", err)
	}
	return nil
}
