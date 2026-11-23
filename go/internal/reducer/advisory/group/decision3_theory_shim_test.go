// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Prove-theory-first shim for eshu-hq/eshu#11080 Decision-3 (part of #9669,
// follow-up of #10839). It measured the two candidate mechanisms with
// A2-packet-shaped fixtures and recorded the before numbers the arbiter
// needed for Q1 (stale-clock semantics) and Q2 (watermark pace). The lane
// ruling (prior result 1, upholding the committed anchor-conflict ruling)
// keeps the post-claim first-deferral clock and rejects the first-data
// fast-path verdict: the stamp stays telemetry/provenance only. The two
// TestDecision3_* tests at the bottom now pin the corrected contract, and
// the three TestDecision3Shim_* probes above keep pinning the before-state
// they measured. The A2 before-state below is the historical record the
// ruling built on, not a live claim about this tree.
//
// A2 before-state under test (packet 2026-11-20T21:24Z..22:24Z, 31 pulls):
// T-lag FAIL (first-claim lag 436.1..11675.7 min vs the 30-min bar),
// T-outcome FAIL (148 of 154 stale-set entries are attempt_count=1 rows
// first-touched in-window), T-R FAIL (R advanced 51.6 min over 60.0 min
// wall = 0.860x; wall-R lag grew 175.2 to 183.6 min; E-R at classification
// is +65.5..+84.2 min for all 148 first-eval rows).
package group

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// decision3Now is the shim's fixed verdict clock.
var decision3Now = time.Date(2026, 11, 20, 22, 24, 0, 0, time.UTC)

// decision3AncientIntent builds the A2 block-1-shaped row: enqueued
// 436.1 min ago (the observed block-1 minimum first-claim lag), claimed
// for the first time at the verdict, and waiting on a watermark that trails
// its bound by 70 min (inside the observed E-R band +65.5..+84.2 min).
// Underlying advisory data predates the verdict by ~10 days (the A2
// origin_scope window Nov 9-10), which the Intent shape carries only as
// enqueue age: there is no first-underlying-data-time field on the wire.
func decision3AncientIntent(claimedAt time.Time) reducercontract.Intent {
	intent := advisoryGroupIntent(EntityKey("CVE-2026-11080-shim"))
	bound := decision3Now.Add(-436*time.Minute - 6*time.Second)
	intent.EnqueuedAt = bound
	intent.AvailableAt = bound
	intent.CycleStartedAt = bound
	intent.AttemptCount = 1
	intent.ClaimedAt = &claimedAt
	intent.Status = reducercontract.IntentStatusClaimed
	return intent
}

func decision3FailureClass(t *testing.T, err error) string {
	t.Helper()
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("deferral error carries no failure class: %v", err)
	}
	return classified.FailureClass()
}

// TestDecision3Shim_Q1FirstEvalDefersNotStale measures candidate 1, the
// stale-clock anchor. An A2-shaped ancient row on its FIRST evaluation
// defers in the non-counting not_ready class under the current post-claim
// clock (post-claim deferral ~0 < 30 min), even though its claim lag is
// 436 min (14.5x the bar) and its bound gap is 70 min. The stale verdict
// lands one deferral cycle later, when the same-cycle marker has aged past
// the bound.
func TestDecision3Shim_Q1FirstEvalDefersNotStale(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := decision3Now
	intent := decision3AncientIntent(now)
	required := intent.CycleStartedAt
	watermark := required.Add(-70 * time.Minute)
	handler := Handler{
		Winners: &fakeWinnersReader{watermark: watermark},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, errEval1 := handler.Handle(ctx, intent)
	if errEval1 == nil {
		t.Fatal("eval1: want a deferral error, the watermark trails the bound by 70 min")
	}
	if got := decision3FailureClass(t, errEval1); got != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("eval1 class = %q, want %q (first deferral: post-claim clock reads ~0)",
			got, WinnersWatermarkNotReadyFailureClass)
	}
	var waiter interface{ FailureDetails() string }
	if !errors.As(errEval1, &waiter) {
		t.Fatalf("eval1 error carries no failure details: %v", errEval1)
	}

	// Second evaluation 31 min later with the marker the queue persisted.
	later := now.Add(31 * time.Minute)
	intentEval2 := intent
	intentEval2.Failure = &reducercontract.FailureRecord{
		FailureClass: WinnersWatermarkNotReadyFailureClass,
		Details:      waiter.FailureDetails(),
	}
	handlerEval2 := handler
	handlerEval2.Now = func() time.Time { return later }
	_, errEval2 := handlerEval2.Handle(ctx, intentEval2)
	if errEval2 == nil {
		t.Fatal("eval2: want a deferral error, the watermark still trails the bound")
	}
	if got := decision3FailureClass(t, errEval2); got != WinnersWatermarkStaleFailureClass {
		t.Fatalf("eval2 class = %q, want %q (same-cycle deferral aged 31 min past the bound)",
			got, WinnersWatermarkStaleFailureClass)
	}
	t.Log("Q1 before-numbers: claim lag 436.1 min (14.5x bar), E-R gap 70 min, " +
		"verdict sequence eval1=not_ready eval2=stale; the WHETHER is forced by the " +
		"gap, the clock anchor only moves the WHEN by one deferral cycle")
}

// TestDecision3Shim_Q1ClockCannotDistinguishAncientFromDelayed shows the
// discrimination gap behind Q1 option (a): a fresh row delayed 436 min in
// queue behind the same 70-min watermark gap reads the identical
// eval1=not_ready sequence as the ancient-data row above. The current
// post-claim clock keys on deferral age, so first-underlying-data time is
// invisible to the verdict; stamping it (Q1 option a) would need a new
// timestamp on the intent/queue wire, while keeping first-evaluation with a
// wider bar (option b) stays handler-local.
func TestDecision3Shim_Q1ClockCannotDistinguishAncientFromDelayed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := decision3Now
	fresh := decision3AncientIntent(now)
	fresh.ScopeID = "scope:fresh-delayed-only"
	handler := Handler{
		Winners: &fakeWinnersReader{watermark: fresh.CycleStartedAt.Add(-70 * time.Minute)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, errFresh := handler.Handle(ctx, fresh)
	if errFresh == nil {
		t.Fatal("fresh-but-delayed: want a deferral error")
	}
	ancient := decision3AncientIntent(now)
	handlerAncient := Handler{
		Winners: &fakeWinnersReader{watermark: ancient.CycleStartedAt.Add(-70 * time.Minute)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, errAncient := handlerAncient.Handle(ctx, ancient)
	if errAncient == nil {
		t.Fatal("ancient-data: want a deferral error")
	}
	if got, want := decision3FailureClass(t, errFresh), decision3FailureClass(t, errAncient); got != want {
		t.Fatalf("verdict differs: fresh-delayed=%q ancient=%q, want identical (clock is data-age-blind)", got, want)
	}
	t.Log("Q1 discrimination gap confirmed: identical verdicts for 10-day-old " +
		"underlying data vs merely queue-delayed rows at the same gap")
}

// TestDecision3Shim_Q2WatermarkPaceTrailsWall measures candidate 2, the
// watermark pace, from the A2 R(t) series: R advanced 51.6 min over 60.0
// min wall (0.860x), so the wall-R lag grew 175.2 to 183.6 min (+8.4 min
// = (1-0.860)*60, closing the arithmetic loop). A pace below 1.0x wall can
// never close a positive E-R gap while intents keep arriving at wall rate:
// every evaluated row in A2 carried E-R +65.5..+84.2 min, so the watermark
// satisfied zero evaluated rows by construction, independent of the Q1
// clock anchor.
func TestDecision3Shim_Q2WatermarkPaceTrailsWall(t *testing.T) {
	t.Parallel()
	const wallMin, rAdvanceMin = 60.0, 51.6
	pace := rAdvanceMin / wallMin
	if pace >= 1.0 {
		t.Fatalf("pace = %.3fx wall, want < 1.0x (the trailing regime under test)", pace)
	}
	const lagOpen, lagClose = 175.2, 183.6
	if got := lagOpen + (1-pace)*wallMin; math.Abs(got-lagClose) > 0.05 {
		t.Fatalf("lag drift = %.2f, want %.2f (arithmetic must close before the theory builds on it)", got, lagClose)
	}
	// Minimum worthwhile improvement: pace that closes the 183.6-min lag
	// inside W minutes while intake advances at wall rate is 1+G/W.
	for _, w := range []float64{60, 120, 360} {
		t.Logf("Q2 before-numbers: pace=%.3fx wall, to drain G=183.6 min in %.0f min needs pace >= %.2fx",
			pace, w, 1+183.6/w)
	}
	// Intake-bound alternative (Q2 option b): cap inflow at what the
	// watermark covers, i.e. pace x the observed ~966/min combined peak.
	t.Logf("Q2 before-numbers: intake-bound alternative caps inflow at <= %.0f/min "+
		"(0.860 x 966/min peak), shedding >= 14%% of observed inflow", pace*966)
}

// TestDecision3_StampedAncientDataStillDefersNotReadyOnFirstEval is the
// failing-first contract for the lane-ruling correction (prior result 1,
// upholding the committed anchor-conflict ruling): the first-data stamp is
// telemetry/provenance only and must NOT drive the stale verdict. A stamped
// ancient row on its FIRST evaluation defers not_ready under the post-claim
// first-deferral clock even though its stamped time is older than
// winnersWatermarkMaxWait; the stale verdict lands one deferral cycle
// later, when the same-cycle marker has aged past the bound. It is RED
// while the fast-path verdict use reads stale on the stamp.
func TestDecision3_StampedAncientDataStillDefersNotReadyOnFirstEval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := decision3Now
	intent := decision3AncientIntent(now)
	// Decision-3 wire retained for telemetry/provenance only: the producer
	// stamps the touching pass's cycle start, and the verdict must ignore
	// it. Rows enqueued before the wire carry no field; both shapes read
	// the same post-claim deferral clock.
	intent.Payload = map[string]any{
		PayloadKeyFirstUnderlyingDataAt: intent.CycleStartedAt.UTC().Format(time.RFC3339Nano),
	}
	handler := Handler{
		Winners: &fakeWinnersReader{watermark: intent.CycleStartedAt.Add(-70 * time.Minute)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, err := handler.Handle(ctx, intent)
	if err == nil {
		t.Fatal("want a deferral error")
	}
	if got := decision3FailureClass(t, err); got != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("first-eval class = %q, want %q (the stamp must not "+
			"drive the verdict; post-claim deferral is ~0)", got, WinnersWatermarkNotReadyFailureClass)
	}
	var waiter interface{ FailureDetails() string }
	if !errors.As(err, &waiter) {
		t.Fatalf("first-eval error carries no failure details: %v", err)
	}

	// Second evaluation 31 min later with the marker the queue persisted:
	// the same-cycle deferral has aged past the bound, so the verdict is
	// stale and counts attempts.
	later := now.Add(31 * time.Minute)
	intentEval2 := intent
	intentEval2.Failure = &reducercontract.FailureRecord{
		FailureClass: WinnersWatermarkNotReadyFailureClass,
		Details:      waiter.FailureDetails(),
	}
	handlerEval2 := handler
	handlerEval2.Now = func() time.Time { return later }
	_, errEval2 := handlerEval2.Handle(ctx, intentEval2)
	if errEval2 == nil {
		t.Fatal("eval2: want a deferral error, the watermark still trails the bound")
	}
	if got := decision3FailureClass(t, errEval2); got != WinnersWatermarkStaleFailureClass {
		t.Fatalf("eval2 class = %q, want %q (same-cycle deferral aged 31 min past the bound)",
			got, WinnersWatermarkStaleFailureClass)
	}
}

// TestDecision3_WatermarkWaitCarriesOriginLabel is the failing-first
// contract for the NVD/GHSA telemetry split on the paced path (#11080
// acceptance criterion): the watermark_waits_total verdict carries the
// advisory origin so NVD-vs-GHSA inflow imbalance is attributable per
// verdict. It was RED before the label existed
// (RecordAdvisoryGroupWatermarkWait had no origin attribute; the
// dirty-tally GHSA bucket from #11078 covers the maintainer drain, not
// this verdict).
func TestDecision3_WatermarkWaitCarriesOriginLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	now := decision3Now
	intent := decision3AncientIntent(now)
	handler := Handler{
		Winners:     &fakeWinnersReader{watermark: intent.CycleStartedAt.Add(-70 * time.Minute)},
		Writer:      &fakeWriter{},
		Instruments: inst,
		Now:         func() time.Time { return now },
	}
	if _, err := handler.Handle(ctx, intent); err == nil {
		t.Fatal("want a deferral error")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &metrics); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_advisory_group_projection_watermark_waits_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if origin, ok := point.Attributes.Value(attribute.Key("origin")); ok {
					t.Logf("origin label present: %q", origin.AsString())
					return
				}
				t.Fatalf("watermark_waits_total point has no " +
					"origin attribute; NVD/GHSA split on the paced path is NOT_CHECKED")
			}
		}
	}
	t.Fatal("no watermark_waits_total sample at all")
}
