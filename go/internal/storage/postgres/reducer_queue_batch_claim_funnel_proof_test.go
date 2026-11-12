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

// TestClaimBatchFunnelReconcilesLive is the #10565 box-6 live proof: the
// funnel census runs pre-claim, so it describes the claim that just ran and
// the admitted batch plus the closed-reason drops reconcile exactly to the
// candidate count. Every expected value below is derived by hand from the
// gate-precompute seed (not from the implementation), at clockB (now+4h)
// with drain on, projN=2, semCap=3:
//
//   - base rows: 22 (every seed row except the two projector-stage rows).
//     gate-blocked fails readiness (no committed phase) → not_ready=1.
//   - reps_ranked: 21 readiness-ok rows; gate-older-pending loses its key
//     to the expired holder (claimed ranks first) → not_representative=1.
//   - projector fence: scope-gate-proj holds live projector work while
//     drain is on → projector_drain=2 (the two fenced rows).
//   - semantic fence: inflight is empty at clockB, so rank <= 3 admits
//     gate-sem-00, gate-sem-01, gate-sem-tie-a; gate-sem-tie-b (same
//     updated_at as tie-a — the tiebreak), gate-sem-02/03, and the two
//     reclaimed live rows refuse → semantic_cap=5.
//   - pre-fence representatives: 22-1-1-2-5 = 13. No live leases at clockB,
//     so the live-lease fence takes nothing → representatives=13, fence=0.
//
// The limited subtest (batch limit 2) proves the limit trim against those
// 13 representatives; the unlimited subtest (limit 1000, representatives <
// limit) proves the full representative set is admitted, including the
// holder of the shadowed holder-key whose older sibling counts as
// not_representative. Skipped unless a DSN is provided, like the other
// claim proofs.
func TestClaimBatchFunnelReconcilesLive(t *testing.T) {
	// wantRepresentatives is the hand-derived pre-fence set above.
	wantRepresentatives := []string{
		"gate-ready",
		"gate-sem-00", "gate-sem-01", "gate-sem-tie-a",
		"gate-holder",
		"gate-lease-edge",
		"gate-plain-00", "gate-plain-01", "gate-plain-02", "gate-plain-03",
		"gate-visible-late",
		"gate-search-00", "gate-search-01",
	}
	for _, tc := range []struct {
		name string
		// limit is the batch-claim $8.
		limit         int
		wantAdmitted  int
		wantOverLimit int64
		wantLockLost  int64
		wantDrops     map[string]int64
	}{
		{
			name:          "limited",
			limit:         2,
			wantAdmitted:  2,
			wantOverLimit: 11,
			wantLockLost:  0,
			wantDrops: map[string]int64{
				telemetry.BatchClaimDropNotReady:          1,
				telemetry.BatchClaimDropNotRepresentative: 1,
				telemetry.BatchClaimDropProjectorDrain:    2,
				telemetry.BatchClaimDropSemanticCap:       5,
				telemetry.BatchClaimDropOverLimit:         11,
			},
		},
		{
			// Representatives (13) below the batch limit: no trim, no
			// lock-time shortfall, every representative admitted.
			name:          "unlimited",
			limit:         1000,
			wantAdmitted:  13,
			wantOverLimit: 0,
			wantLockLost:  0,
			wantDrops: map[string]int64{
				telemetry.BatchClaimDropNotReady:          1,
				telemetry.BatchClaimDropNotRepresentative: 1,
				telemetry.BatchClaimDropProjectorDrain:    2,
				telemetry.BatchClaimDropSemanticCap:       5,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

			clock := now.Add(4 * time.Hour)
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
			intents, err := queue.ClaimBatch(ctx, tc.limit)
			if err != nil {
				t.Fatalf("ClaimBatch: %v", err)
			}
			if len(intents) != tc.wantAdmitted {
				t.Fatalf("ClaimBatch admitted %d intents, want %d", len(intents), tc.wantAdmitted)
			}
			admitted := map[string]bool{}
			for _, intent := range intents {
				admitted[intent.IntentID] = true
			}
			for _, id := range wantRepresentatives {
				if tc.name == "unlimited" && !admitted[id] {
					t.Errorf("unlimited claim did not admit representative %s", id)
				}
			}
			if tc.name == "unlimited" {
				// Shadow-sibling key: the holder was just admitted as its
				// key's representative while the older pending sibling
				// counts as not_representative.
				if !admitted["gate-holder"] {
					t.Error("unlimited claim did not admit gate-holder, the representative of the shadowed holder-key")
				}
				if admitted["gate-older-pending"] {
					t.Error("unlimited claim admitted gate-older-pending, the shadowed sibling of the holder-key")
				}
			}
			for id := range admitted {
				found := false
				for _, want := range wantRepresentatives {
					if id == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("claim admitted %s, outside the hand-derived representative set", id)
				}
			}

			var collected metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &collected); err != nil {
				t.Fatalf("Collect: %v", err)
			}
			candidates, ok := funnelHistogramSum(collected, "reducer_batch_claim_candidates")
			if !ok || candidates != 22 {
				t.Fatalf("candidates histogram = (%d, %v), want (22, true)", candidates, ok)
			}
			representatives, ok := funnelHistogramSum(collected, "reducer_batch_claim_representatives")
			if !ok || representatives != 13 {
				t.Fatalf("representatives histogram = (%d, %v), want (13, true)", representatives, ok)
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
			if len(drops) != len(tc.wantDrops) {
				t.Fatalf("drop reasons = %v, want exactly %v", drops, tc.wantDrops)
			}
			var accounted int64
			for reason, want := range tc.wantDrops {
				got, ok := drops[reason]
				if !ok {
					t.Fatalf("drop reason %q missing from %v", reason, drops)
				}
				if got != want {
					t.Fatalf("drop reason %q = %d, want %d", reason, got, want)
				}
				accounted += got
			}
			if int64(len(intents))+accounted != candidates {
				t.Fatalf("funnel does not reconcile: admitted %d + drops %v = %d, want candidates %d",
					len(intents), drops, int64(len(intents))+accounted, candidates)
			}
			t.Logf("funnel reconciles live (%s): candidates=%d admitted=%d representatives=%d drops=%v",
				tc.name, candidates, len(intents), representatives, drops)
		})
	}
}

// funnelHistogramSum reads one batch-claim funnel histogram's sum.
func funnelHistogramSum(collected metricdata.ResourceMetrics, suffix string) (int64, bool) {
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			histogram, ok := metric.Data.(metricdata.Histogram[int64])
			if !ok || !strings.HasSuffix(metric.Name, suffix) {
				continue
			}
			if len(histogram.DataPoints) != 1 {
				return 0, false
			}
			return int64(histogram.DataPoints[0].Sum), true
		}
	}
	return 0, false
}
