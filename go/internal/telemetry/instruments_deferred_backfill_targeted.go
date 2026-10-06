// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerDeferredBackfillTargeted registers the partition-scoped deferred
// maintenance instruments (#7584) on inst. Labels are closed sets:
//
//   - eshu_dp_deferred_backfill_targeted_duration_seconds{outcome}: one sample
//     per pass; outcome is "completed", a refusal reason ("catalog_changed",
//     "no_memo_baseline", "closure_too_deep"), or "error".
//   - eshu_dp_deferred_backfill_targeted_outcomes_total{outcome}: one count per
//     owed partition ("published", "not_active", "inapplicable", "retry") and
//     one per refused or failed pass (the pass outcomes above).
//   - eshu_dp_deferred_backfill_targeted_reopened_total{domain}: work items the
//     pass reopened, by reducer domain (deployment_mapping,
//     code_import_repo_edge, and each cross-scope correlation domain).
func registerDeferredBackfillTargeted(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.DeferredBackfillTargetedDuration, err = meter.Float64Histogram(
		"eshu_dp_deferred_backfill_targeted_duration_seconds",
		metric.WithDescription("Wall time of one partition-scoped deferred relationship maintenance pass (#7584), by pass outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120),
	); err != nil {
		return fmt.Errorf("register DeferredBackfillTargetedDuration histogram: %w", err)
	}
	if inst.DeferredBackfillTargetedOutcomes, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_targeted_outcomes_total",
		metric.WithDescription("Partition-scoped deferred maintenance results (#7584): one per owed partition (published, not_active, inapplicable, retry) and one per refused or failed pass (catalog_changed, no_memo_baseline, closure_too_deep, error)"),
	); err != nil {
		return fmt.Errorf("register DeferredBackfillTargetedOutcomes counter: %w", err)
	}
	if inst.DeferredBackfillTargetedReopened, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_targeted_reopened_total",
		metric.WithDescription("Succeeded reducer work items the partition-scoped deferred maintenance pass reopened (#7584), by domain"),
	); err != nil {
		return fmt.Errorf("register DeferredBackfillTargetedReopened counter: %w", err)
	}
	return nil
}
