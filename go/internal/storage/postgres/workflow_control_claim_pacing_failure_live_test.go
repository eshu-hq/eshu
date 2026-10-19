// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8473 pacing failure-story proofs on real PostgreSQL: latch-signal loss fails open with the outage named, an overdue engagement re-reads depth and releases only below the release watermark, and every overdue verdict emits the stuck operator signal
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pacingFailureSelector is the claiming identity every pacing failure-story
// proof consults with: one family on the shared claim-depth instance.
func pacingFailureSelector() workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}
}

// TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure proves the second
// fail-open leg of the #8473 depth-loss story: the depth scan succeeds
// against a deep backlog that would pace, but the latch signal is gone (its
// table renamed away), so the consult reports the outage and an unpaced
// verdict instead of pacing on a partial reading.
// TestWorkflowClaimPacingFailsOpenOnDepthQueryFailure covers the depth leg
// with a dead pool; this covers the latch leg with live depth.
func TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure(t *testing.T) {
	database, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "pacing-latch-failopen", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := pacingFailureSelector()

	engaged, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err != nil || !engaged.Paced {
		t.Fatalf("pre-break CheckClaimPacing() = %+v, %v; want paced (60 pending over domain high 50)", engaged, err)
	}

	if _, err := database.ExecContext(ctx, `ALTER TABLE workflow_claim_pacing RENAME TO workflow_claim_pacing_broken`); err != nil {
		t.Fatalf("rename latch table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := database.ExecContext(context.Background(), `ALTER TABLE workflow_claim_pacing_broken RENAME TO workflow_claim_pacing`); err != nil {
			t.Errorf("restore latch table: %v", err)
		}
	})

	decision, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err == nil {
		t.Fatal("CheckClaimPacing() with a missing latch table error = nil, want the latch outage surfaced")
	}
	if !strings.Contains(err.Error(), "workflow_claim_pacing") {
		t.Fatalf("CheckClaimPacing() error = %v, want it to name the broken latch signal", err)
	}
	if decision.Paced {
		t.Fatalf("CheckClaimPacing() = %+v, want unpaced (fail open on a missing latch reading)", decision)
	}
	if decision.Stuck {
		t.Fatalf("CheckClaimPacing() = %+v, want no stuck signal on a failed consult", decision)
	}
}

// TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark proves the
// design's timer-expiry shape on live storage: an overdue engagement re-reads
// depth instead of releasing on the timer. With 30 pending sitting between
// the low 20 and high 50 watermarks past MaxEngagement, the verdict stays
// paced and loud; only once depth drops below the release watermark does the
// same overdue engagement release. The mid-band-overdue case is the one
// neither TestWorkflowClaimPacingStuckSignal (overdue extremes) nor
// TestWorkflowClaimPacingHysteresisRelease (a fresh latch) covers.
func TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "pacing-stuck-mid", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := pacingFailureSelector()
	engagedAt := time.Now().UTC()

	first, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt)
	if err != nil || !first.Paced || first.Stuck {
		t.Fatalf("engage = %+v, %v; want paced without stuck", first, err)
	}

	// Drain to 30: mid band, still above the domain low 20. The overdue
	// consult must see the re-read 30 — not the 60 at engagement and not a
	// blind timer release — and stay engaged and loud.
	completeClaimDepthItems(t, store, ctx, "pacing-owner", 30)
	mid, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(2*cfg.MaxEngagement))
	if err != nil {
		t.Fatalf("mid-band overdue CheckClaimPacing() error = %v, want nil", err)
	}
	if !mid.Paced || !mid.Stuck {
		t.Fatalf("mid-band overdue engagement = %+v, want paced with stuck (release needs below low 20)", mid)
	}
	if mid.RetryAfter != cfg.GrantDelay {
		t.Fatalf("mid-band overdue RetryAfter = %s, want grant delay %s", mid.RetryAfter, cfg.GrantDelay)
	}

	// Drain to 5: below the low watermark. The same overdue engagement now
	// releases, proving the stuck verdict followed the re-read depth down.
	completeClaimDepthItems(t, store, ctx, "pacing-owner", 25)
	released, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(3*cfg.MaxEngagement))
	if err != nil {
		t.Fatalf("drained overdue CheckClaimPacing() error = %v, want nil", err)
	}
	if released.Paced || released.Stuck {
		t.Fatalf("drained overdue engagement = %+v, want released without stuck", released)
	}
}

// pacingSignalCounterPoints collects one int64-sum pacing instrument keyed by
// "pacing_signal + NUL + domain", so a wedged throttle's operator signals
// assert on values, not on the verdict struct alone.
func pacingSignalCounterPoints(t *testing.T, reader *sdkmetric.ManualReader, metric string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	points := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", metric, m.Data)
			}
			for _, p := range sum.DataPoints {
				signal, _ := p.Attributes.Value(attribute.Key(telemetry.MetricDimensionPacingSignal))
				domain, _ := p.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				points[signal.AsString()+"\x00"+domain.AsString()] += p.Value
			}
		}
	}
	return points
}

// pacingDelaySamples counts the grant-delay histogram samples and their total
// issued delay, pinning both that every paced grant waited and for how long.
func pacingDelaySamples(t *testing.T, reader *sdkmetric.ManualReader) (count uint64, sum float64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_workflow_claim_pacing_delay_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("delay histogram data = %T, want Histogram[float64]", m.Data)
			}
			for _, p := range hist.DataPoints {
				count += p.Count
				sum += p.Sum
			}
		}
	}
	return count, sum
}

// TestWorkflowClaimPacingStuckEmitsOperatorSignals proves a wedged throttle
// stays loud at the metric level, not just in the verdict struct: one
// engagement fires the engaged counter and the active gauge exactly once,
// every overdue verdict fires the stuck counter, every paced grant records
// its delay, and the drain releases the gauge back to zero. No existing proof
// wires instruments to these series; the dashboard panels read them.
func TestWorkflowClaimPacingStuckEmitsOperatorSignals(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store.Instruments = inst
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "pacing-stuck-signals", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := pacingFailureSelector()
	engagedAt := time.Now().UTC()

	first, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt)
	if err != nil || !first.Paced || first.Stuck {
		t.Fatalf("engage = %+v, %v; want paced without stuck", first, err)
	}
	for i := 2; i <= 3; i++ {
		overdue, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(time.Duration(i)*cfg.MaxEngagement))
		if err != nil {
			t.Fatalf("overdue CheckClaimPacing() error = %v, want nil", err)
		}
		if !overdue.Paced || !overdue.Stuck {
			t.Fatalf("overdue engagement %d = %+v, want paced with stuck", i, overdue)
		}
		if overdue.RetryAfter != cfg.GrantDelay {
			t.Fatalf("overdue engagement %d RetryAfter = %s, want grant delay %s", i, overdue.RetryAfter, cfg.GrantDelay)
		}
	}

	completeClaimDepthItems(t, store, ctx, "pacing-owner", len(seeded))
	drained, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(4*cfg.MaxEngagement))
	if err != nil {
		t.Fatalf("drained CheckClaimPacing() error = %v, want nil", err)
	}
	if drained.Paced || drained.Stuck {
		t.Fatalf("drained overdue engagement = %+v, want released without stuck", drained)
	}

	const series = "domain\x00git/claim-depth-instance"
	if got := pacingSignalCounterPoints(t, reader, "eshu_dp_workflow_claim_pacing_engaged_total"); got[series] != 1 {
		t.Fatalf("engaged_total %q points = %v, want exactly 1 (one latch engagement)", series, got)
	}
	if got := pacingSignalCounterPoints(t, reader, "eshu_dp_workflow_claim_pacing_stuck_total"); got[series] != 2 {
		t.Fatalf("stuck_total %q points = %v, want 2 (one per overdue verdict)", series, got)
	}
	active, ok := pacingSignalCounterPoints(t, reader, "eshu_dp_workflow_claim_pacing_active")[series]
	if !ok || active != 0 {
		t.Fatalf("active gauge %q = %d (present %v), want the series present at net 0 (engage +1, release -1)", series, active, ok)
	}
	count, sum := pacingDelaySamples(t, reader)
	if count != 3 {
		t.Fatalf("delay histogram samples = %d, want 3 (one per paced grant)", count)
	}
	if want := 3 * cfg.GrantDelay.Seconds(); sum < want-1e-9 || sum > want+1e-9 {
		t.Fatalf("delay histogram sum = %v, want %v (3 grants at the configured delay)", sum, want)
	}
}
