// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionCoverageState labels
// eshu_dp_query_security_alert_empty_answers_total with the coverage state an
// empty security-alert answer reported (#8803). The value set is closed:
// complete, target_incomplete, or not_configured.
const MetricDimensionCoverageState = "coverage_state"

// AttrCoverageState returns a coverage_state attribute. v must be complete,
// target_incomplete, or not_configured.
func AttrCoverageState(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionCoverageState, v)
}

// registerSecurityAlertQueryInstruments registers the empty security-alert
// answer counter on inst (#8803).
func registerSecurityAlertQueryInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.QuerySecurityAlertEmptyAnswers, err = meter.Int64Counter(
		"eshu_dp_query_security_alert_empty_answers_total",
		metric.WithDescription("Empty security-alert reconciliation answers (list, count, inventory) classified from collector state, by coverage_state (complete, target_incomplete, not_configured) and reason (none for a healthy empty answer; otherwise collector_not_configured, repository_outside_target_set, no_committed_generation, collection_state_unavailable, or terminal_fetch_failure:<auth_denied|not_found|rate_limited|terminal|unclassified>)"),
	); err != nil {
		return fmt.Errorf("register QuerySecurityAlertEmptyAnswers counter: %w", err)
	}
	return nil
}
