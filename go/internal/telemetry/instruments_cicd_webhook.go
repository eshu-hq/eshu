// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCICDRunWebhookInstruments records the bounded Actions wake-up
// signals used by the workflow coordinator. No repository, delivery, or run
// identifiers are metric labels.
func registerCICDRunWebhookInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.CICDRunWebhookHandoffs, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_webhook_handoffs_total",
		metric.WithDescription("Verified GitHub Actions webhook handoff attempts by closed outcome: admitted, recovered, blocked, unauthorized, or failed"),
	)
	if err != nil {
		return fmt.Errorf("register CICDRunWebhookHandoffs counter: %w", err)
	}
	inst.CICDRunWebhookReaps, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_webhook_reaps_total",
		metric.WithDescription("Expired GitHub Actions delivery leases by closed reaper outcome: requeued or exhausted"),
	)
	if err != nil {
		return fmt.Errorf("register CICDRunWebhookReaps counter: %w", err)
	}
	inst.CICDRunPollFallback, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_poll_fallback_total",
		metric.WithDescription("Scheduled CI/CD run target work admitted as webhook backfill or fallback"),
	)
	if err != nil {
		return fmt.Errorf("register CICDRunPollFallback counter: %w", err)
	}
	inst.CICDRunWebhookLag, err = meter.Float64Histogram(
		"eshu_dp_ci_cd_run_webhook_lag_seconds",
		metric.WithDescription("Seconds from a verified GitHub Actions delivery receipt to durable workflow handoff admission or exact-work recovery"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0, 1, 5, 15, 30, 60, 120, 300, 900, 3600, 21600),
	)
	if err != nil {
		return fmt.Errorf("register CICDRunWebhookLag histogram: %w", err)
	}
	return nil
}
