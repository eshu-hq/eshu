// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AttrWorkflowStatsAnalyzeSite returns a site attribute for the
// post-admission stats ANALYZE counter. v must be one of the
// contract.WorkflowStatsAnalyzeSite* constants.
func AttrWorkflowStatsAnalyzeSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSite, v)
}

// registerWorkflowClaimStatsInstruments registers the #9871 post-admission
// stats ANALYZE counter on inst: one increment per admission that reached
// the statistics gate with rows admitted, labeled by closed site and
// outcome sets, with no scope, run, or work-item id.
func registerWorkflowClaimStatsInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.WorkflowWorkItemsStatsAnalyze, err = meter.Int64Counter(
		"eshu_dp_workflow_work_items_stats_analyze_total",
		metric.WithDescription("Post-admission ANALYZE (SKIP_LOCKED) on workflow_work_items by admission site (enqueue, guarded, scanner) and outcome (analyzed, skipped, failed) (#9871)"),
	); err != nil {
		return fmt.Errorf("register WorkflowWorkItemsStatsAnalyze counter: %w", err)
	}
	return nil
}
