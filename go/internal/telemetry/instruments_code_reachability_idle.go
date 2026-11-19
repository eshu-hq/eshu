// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Closed kind values for eshu_dp_code_reachability_projection_cycles_total
// (#10870). Every runner cycle records exactly one kind, so the counter is
// the issue's idle-cost signal: probe_skip cycles cost one index-only probe,
// every other kind cost one full candidate query.
const (
	// CodeReachabilityCycleKindProbeSkip means the cursor probe had not
	// advanced (and no audit was due): the full candidate query did not run.
	CodeReachabilityCycleKindProbeSkip = "probe_skip"
	// CodeReachabilityCycleKindProbeTriggeredFull means a probe advance past
	// the cursor triggered the full candidate query.
	CodeReachabilityCycleKindProbeTriggeredFull = "probe_triggered_full"
	// CodeReachabilityCycleKindAuditFull means the periodic audit forced the
	// full candidate query (startup/unknown cursor, probe error, probe
	// regression, or audit interval elapsed).
	CodeReachabilityCycleKindAuditFull = "audit_full"
	// CodeReachabilityCycleKindDrainFull means the previous full query was
	// productive and the probe has not advanced: the backlog may hold more
	// than one batch, so the cycle drains it with another full query. It is
	// not an audit — the probe path forced the query, and it carries no
	// audit log line.
	CodeReachabilityCycleKindDrainFull = "drain_full"
	// CodeReachabilityCycleKindProductive means the full candidate query
	// found snapshots to project.
	CodeReachabilityCycleKindProductive = "productive"
)

// Closed trigger values for
// eshu_dp_code_reachability_projection_inputs_found_total (#10870): which
// full-query path surfaced the projected snapshots.
const (
	// CodeReachabilityTriggerProbe means the probe path (advance or backlog
	// drain) ran the full query that found the inputs — not the audit
	// backstop.
	CodeReachabilityTriggerProbe = "probe"
	// CodeReachabilityTriggerAudit means the audit forced the full query
	// that found the inputs — including the short-list-miss class the
	// audit backstop exists to catch.
	CodeReachabilityTriggerAudit = "audit"
)

// registerCodeReachabilityIdle registers the #10870 code-reachability
// idle-probe instruments on inst. Labels are closed bounded sets: kind (the
// four cycle kinds above) and trigger (probe, audit). No scope, generation,
// repository, or run value ever rides a label.
func registerCodeReachabilityIdle(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CodeReachabilityProjectionCycles, err = meter.Int64Counter(
		"eshu_dp_code_reachability_projection_cycles_total",
		metric.WithDescription("Code-reachability projection runner cycles by kind (probe_skip, probe_triggered_full, audit_full, drain_full, productive); only non-skip kinds run the full candidate query (#10870)"),
	); err != nil {
		return fmt.Errorf("register CodeReachabilityProjectionCycles counter: %w", err)
	}
	if inst.CodeReachabilityProjectionInputsFound, err = meter.Int64Counter(
		"eshu_dp_code_reachability_projection_inputs_found_total",
		metric.WithDescription("Code-reachability projection inputs found by the full candidate query, by trigger (probe, audit); an audit rate above zero is the short-list-miss class (#10870)"),
	); err != nil {
		return fmt.Errorf("register CodeReachabilityProjectionInputsFound counter: %w", err)
	}
	if inst.CodeReachabilityProjectionBackoff, err = meter.Float64Gauge(
		"eshu_dp_code_reachability_projection_backoff_seconds",
		metric.WithDescription("Current code-reachability projection idle backoff delay in seconds; 0 means a productive cycle re-polled immediately (#10870)"),
	); err != nil {
		return fmt.Errorf("register CodeReachabilityProjectionBackoff gauge: %w", err)
	}
	return nil
}
