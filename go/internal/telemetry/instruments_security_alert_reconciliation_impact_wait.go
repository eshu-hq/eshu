// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerSecurityAlertReconciliationImpactWait registers the #8827 counter of
// security alert reconciliation impact-wait decisions, by domain and outcome.
func registerSecurityAlertReconciliationImpactWait(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SecurityAlertReconciliationImpactWait, err = meter.Int64Counter(
		"eshu_dp_security_alert_reconciliation_impact_wait_total",
		metric.WithDescription("Security alert reconciliation impact-wait decisions by reducer domain and outcome "+
			"(deferred_in_flight/gave_up_unmatched/gave_up_bound_exceeded): whether a reconciliation waiting on "+
			"impact evidence deferred without spending an attempt because the same scope generation's "+
			"supply_chain_impact work was in flight, or gave up at its attempt cap and wrote unmatched decisions (#8827)"),
	); err != nil {
		return fmt.Errorf("register SecurityAlertReconciliationImpactWait counter: %w", err)
	}
	return nil
}
