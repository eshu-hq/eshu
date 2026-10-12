// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerReconcileParseSkipDecisions registers the pre-parse skip decision
// counter (#7978 follow-up, ruling a9-143). One increment per forced reconcile
// the git collector evaluates, labeled outcome: skipped when the parse was
// skipped and the active generation re-projected, or the reason it parsed
// (reason_ineligible, disabled, no_active_full, delta_active, head_moved,
// identity_absent, identity_mismatch, submodules_present, dirty_tree,
// valve_due, read_error).
func registerReconcileParseSkipDecisions(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReconcileParseSkipDecisions, err = meter.Int64Counter(
		"eshu_dp_collector_reconcile_parse_skip_decisions_total",
		metric.WithDescription("Pre-parse skip decisions of forced git reconciles by outcome (#7978)"),
	); err != nil {
		return fmt.Errorf("register ReconcileParseSkipDecisions counter: %w", err)
	}
	return nil
}
