// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCollectorTerminalFailureAcknowledgements registers the operator
// acknowledgement outcome counter for collector terminal failures (#8830) on
// inst.
func registerCollectorTerminalFailureAcknowledgements(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CollectorTerminalFailureAcknowledgements, err = meter.Int64Counter(
		"eshu_dp_collector_terminal_failure_acknowledgements_total",
		metric.WithDescription("Operator acknowledgement requests for a collector terminal failure, by closed outcome (#8830)"),
		metric.WithUnit("{acknowledgement}"),
	); err != nil {
		return fmt.Errorf("register CollectorTerminalFailureAcknowledgements counter: %w", err)
	}
	return nil
}
