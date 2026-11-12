// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestClaimBatchFunnelReconcilesLive is the #10565 box-6 live proof: on the
// gate-precompute fixture (every admission gate exercised), one ClaimBatch
// with real instruments must reconcile exactly — admitted + the closed-reason
// drops == candidates in — with representatives matching the candidate CTE.
// A funnel that counts a different membership than the claim admits fails
// here. Skipped unless a DSN is provided, like the other claim proofs.
func TestClaimBatchFunnelReconcilesLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the batch-claim funnel proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGatePrecomputeFixture(t, ctx, db, now)

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("batch-claim-funnel-live-proof"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	clock := now.Add(2 * time.Hour)
	queue := ReducerQueue{
		database:                         SQLDB{DB: db},
		LeaseOwner:                       "batch-claim-funnel-live-proof",
		LeaseDuration:                    time.Hour,
		Now:                              func() time.Time { return clock },
		RequireProjectorDrainBeforeClaim: true,
		ExpectedSourceLocalProjectors:    2,
		SemanticEntityClaimLimit:         3,
		Instruments:                      instruments,
	}
	intents, err := queue.ClaimBatch(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	admitted := int64(len(intents))
	if admitted == 0 {
		t.Fatal("ClaimBatch on the gate fixture admitted nothing; the funnel has no representatives to reconcile")
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	histogramSum := func(suffix string) (int64, bool) {
		for _, scope := range collected.ScopeMetrics {
			for _, metric := range scope.Metrics {
				histogram, ok := metric.Data.(metricdata.Histogram[int64])
				if !ok || !strings.HasSuffix(metric.Name, suffix) {
					continue
				}
				if len(histogram.DataPoints) != 1 {
					t.Fatalf("%s data points = %d, want 1", suffix, len(histogram.DataPoints))
				}
				return int64(histogram.DataPoints[0].Sum), true
			}
		}
		return 0, false
	}
	candidates, ok := histogramSum("reducer_batch_claim_candidates")
	if !ok || candidates == 0 {
		t.Fatalf("candidates histogram = (%d, %v), want a nonzero point", candidates, ok)
	}
	representatives, ok := histogramSum("reducer_batch_claim_representatives")
	if !ok {
		t.Fatal("representatives histogram recorded nothing for the claim")
	}

	drops := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok || !strings.HasSuffix(metric.Name, "reducer_batch_claim_drops_total") {
				continue
			}
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value("reason")
				if !ok {
					t.Fatalf("drops point without a reason attribute: %+v", point.Attributes)
				}
				drops[value.AsString()] = int64(point.Value)
			}
		}
	}
	var accounted int64
	for _, reason := range telemetry.BatchClaimDropReasons() {
		accounted += drops[reason]
	}
	if admitted+accounted != candidates {
		t.Fatalf("funnel does not reconcile: admitted %d + drops %v = %d, want candidates %d",
			admitted, drops, admitted+accounted, candidates)
	}
	if representatives > candidates {
		t.Fatalf("representatives %d exceed candidates %d", representatives, candidates)
	}
	t.Logf("funnel reconciles live: candidates=%d admitted=%d representatives=%d drops=%v",
		candidates, admitted, representatives, drops)
}
