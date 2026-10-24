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
		metric.WithDescription("Empty security-alert reconciliation answers (list, count, inventory) classified from collector state, by coverage_state (complete, target_incomplete, not_configured) and reason (none for a healthy empty answer; otherwise collector_not_configured, repository_outside_target_set, no_committed_generation, collector_state_unavailable, no_authorized_repositories, org_repository_fanout_cap_reached, org_repository_read_rate_limited, org_repository_read_failed, org_repository_fanout_stopped, org_repository_alerts_disabled, provider_open_alert_page_limit_reached, org_repository_read_incomplete, or terminal_fetch_failure:<auth_denied|not_found|rate_limited|terminal|retryable|attempt_budget_exhausted|identity_mismatch|collect_failure|commit_failure|unclassified>)"),
	); err != nil {
		return fmt.Errorf("register QuerySecurityAlertEmptyAnswers counter: %w", err)
	}
	return nil
}

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
