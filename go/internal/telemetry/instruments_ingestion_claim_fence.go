// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// ingestionCommitPinSlackBuckets bound the pin slack histogram. The default
// lease is 60 s with a re-pin every 20 s and a 5 s refusal margin, so healthy
// slack sits between 40 s and 60 s; samples under 5 s are refusals and the
// low buckets show commits creeping toward the margin.
var ingestionCommitPinSlackBuckets = []float64{0, 1, 2.5, 5, 10, 20, 30, 40, 50, 60, 120}

// registerIngestionClaimFenceInstruments registers the #9376 claim-plane
// commit fence instruments on inst: the rejection counter (label reason),
// the re-pin counter (label outcome), and the unlabeled slack histogram.
func registerIngestionClaimFenceInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.IngestionClaimFenceRejections, err = meter.Int64Counter(
		"eshu_dp_ingestion_claim_fence_rejections_total",
		metric.WithDescription("Claim-plane ingestion commits refused by the cross-service commit fence (#9376), "+
			"by reason: stale_fence, not_found, fence_token, fence_claim_mismatch (terminal) or "+
			"deadline, pin_unavailable (retryable)"),
	); err != nil {
		return fmt.Errorf("register IngestionClaimFenceRejections counter: %w", err)
	}
	if inst.IngestionCommitRepins, err = meter.Int64Counter(
		"eshu_dp_ingestion_commit_repins_total",
		metric.WithDescription("Claim-plane pin extensions during one ingestion commit (#9376), by outcome: "+
			"ok, stale (the commit is cancelled), error (the pre-COMMIT deadline check decides)"),
	); err != nil {
		return fmt.Errorf("register IngestionCommitRepins counter: %w", err)
	}
	if inst.IngestionCommitPinSlack, err = meter.Float64Histogram(
		"eshu_dp_ingestion_commit_pin_slack_seconds",
		metric.WithDescription("Latest claim-plane pin deadline minus the Go clock at the pre-COMMIT check of a "+
			"claim-plane ingestion commit (#9376); a sample below the 5 s margin is a deadline refusal"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(ingestionCommitPinSlackBuckets...),
	); err != nil {
		return fmt.Errorf("register IngestionCommitPinSlack histogram: %w", err)
	}
	return nil
}

// registerIngestionClaimFence appends the claim-plane commit fence span
// (#9376) to the frozen span list.
func registerIngestionClaimFence() {
	spanNames = append(spanNames, contract.SpanIngestionCommitClaimFence)
}
