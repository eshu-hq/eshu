// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// markerLossMetricName is the #11089 marker-loss counter: it fires when a
// row whose prior failure class is a winners-watermark wait class carries
// no usable same-cycle first-deferral marker, so an operator can tell a
// lost marker apart from a genuine first deferral.
const markerLossMetricName = "eshu_dp_advisory_group_projection_watermark_deferral_marker_missing_total"

// markerLossTestNow is 45 minutes into the cycle: past the stale bound by
// row age, so every case below also proves the marker fallback keeps the
// pre-#11075 born-stale behavior out — a lost marker restarts at now and
// defers not_ready, exactly as before #11089.
var markerLossTestNow = handlerTestEnqueuedAt.Add(45 * time.Minute)

func markerDetails(firstDeferredAt, cycleStartedAt time.Time) string {
	return "advisory group projection waiting for supply chain impact winners\n" +
		winnersWatermarkDeferralMarkerPrefix +
		firstDeferredAt.UTC().Format(time.RFC3339Nano) +
		" cycle-started-at=" + cycleStartedAt.UTC().Format(time.RFC3339Nano)
}

func markerLossIntent(priorClass, details string) reducercontract.Intent {
	intent := advisoryGroupIntent(EntityKey("CVE-2026-marker-loss"))
	intent.Failure = &reducercontract.FailureRecord{
		FailureClass: priorClass,
		Message:      "prior verdict",
		Details:      details,
	}
	return intent
}

type markerLossKey struct {
	failureClass string
	reason       string
}

func collectMarkerLoss(t *testing.T, reader *sdkmetric.ManualReader) map[markerLossKey]int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[markerLossKey]int64{}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != markerLossMetricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s: want Int64 sum data, got %T", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				domain, _ := point.Attributes.Value(attribute.Key("domain"))
				if domain.AsString() != string(reducercontract.DomainAdvisoryGroupProjection) {
					t.Fatalf("marker-loss sample domain = %q, want the advisory group domain", domain.AsString())
				}
				class, _ := point.Attributes.Value(attribute.Key("failure_class"))
				reason, _ := point.Attributes.Value(attribute.Key("reason"))
				got[markerLossKey{failureClass: class.AsString(), reason: reason.AsString()}] += point.Value
			}
		}
	}
	return got
}

// TestAdvisoryGroupHandlerWatermarkMarkerLossIsObservable pins #11089: the
// marker-loss counter fires exactly when the prior failure class is a
// watermark-wait class and the same-cycle marker is missing, unparsable,
// or future-dated — and stays silent on a genuine first deferral, a
// healthy marker, a non-wait prior class, and a parsable prior-cycle
// marker (a designed reopen restart, not a loss). Every case also pins
// that the verdict is unchanged: not_ready, an expected wait, and the
// fresh clock anchored at now for the fallback cases.
func TestAdvisoryGroupHandlerWatermarkMarkerLossIsObservable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cycleStart := handlerTestEnqueuedAt
	now := markerLossTestNow

	tests := []struct {
		name       string
		intent     reducercontract.Intent
		wantReason string // empty: the counter must stay silent
	}{
		{
			name:       "missing marker after not_ready fires",
			intent:     markerLossIntent(WinnersWatermarkNotReadyFailureClass, "advisory group projection waiting for supply chain impact winners"),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossMissing,
		},
		{
			name:       "missing marker after stale fires with the prior class",
			intent:     markerLossIntent(WinnersWatermarkStaleFailureClass, ""),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossMissing,
		},
		{
			name: "unparsable first-deferred timestamp fires",
			intent: markerLossIntent(WinnersWatermarkNotReadyFailureClass,
				"waiting\n"+winnersWatermarkDeferralMarkerPrefix+"not-a-time cycle-started-at="+cycleStart.UTC().Format(time.RFC3339Nano)),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossUnparsable,
		},
		{
			name: "unparsable marker shape fires",
			intent: markerLossIntent(WinnersWatermarkNotReadyFailureClass,
				"waiting\n"+winnersWatermarkDeferralMarkerPrefix+cycleStart.UTC().Format(time.RFC3339Nano)),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossUnparsable,
		},
		{
			name: "unparsable cycle timestamp fires",
			intent: markerLossIntent(WinnersWatermarkNotReadyFailureClass,
				"waiting\n"+winnersWatermarkDeferralMarkerPrefix+cycleStart.UTC().Format(time.RFC3339Nano)+" cycle-started-at=not-a-time"),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossUnparsable,
		},
		{
			name:       "future-dated marker fires",
			intent:     markerLossIntent(WinnersWatermarkNotReadyFailureClass, markerDetails(now.Add(5*time.Minute), cycleStart)),
			wantReason: telemetry.AdvisoryGroupWatermarkMarkerLossFutureDated,
		},
		{
			name:   "genuine first deferral stays silent",
			intent: advisoryGroupIntent(EntityKey("CVE-2026-marker-loss")),
		},
		{
			name:   "non-wait prior class stays silent",
			intent: markerLossIntent("reducer_retryable", "read advisory group winners watermark: boom"),
		},
		{
			name:   "healthy marker stays silent",
			intent: markerLossIntent(WinnersWatermarkNotReadyFailureClass, markerDetails(now.Add(-5*time.Minute), cycleStart)),
		},
		{
			name: "prior-cycle marker stays silent",
			intent: markerLossIntent(WinnersWatermarkNotReadyFailureClass,
				markerDetails(cycleStart.Add(-8*24*time.Hour), cycleStart.Add(-8*24*time.Hour))),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			handler := Handler{
				Winners:     &fakeWinnersReader{watermark: cycleStart.Add(-time.Hour)},
				Writer:      &fakeWriter{},
				Instruments: inst,
				Now:         func() time.Time { return now },
			}
			_, handleErr := handler.Handle(ctx, test.intent)
			if handleErr == nil {
				t.Fatal("want a deferral error behind the watermark")
			}
			if !reducercontract.IsExpectedWait(handleErr) {
				t.Fatalf("verdict must stay an expected wait (not_ready), got %v", handleErr)
			}
			var classified interface{ FailureClass() string }
			if !errors.As(handleErr, &classified) || classified.FailureClass() != WinnersWatermarkNotReadyFailureClass {
				t.Fatalf("failure class = %v, want %q: #11089 changes no verdict", handleErr, WinnersWatermarkNotReadyFailureClass)
			}
			if test.wantReason != "" {
				var detailed interface{ FailureDetails() string }
				if !errors.As(handleErr, &detailed) {
					t.Fatalf("wait error must carry failure details: %v", handleErr)
				}
				wantAnchor := winnersWatermarkDeferralMarkerPrefix + now.UTC().Format(time.RFC3339Nano)
				if !strings.Contains(detailed.FailureDetails(), wantAnchor) {
					t.Fatalf("fallback clock must anchor at now (%q), details = %q", wantAnchor, detailed.FailureDetails())
				}
			}

			got := collectMarkerLoss(t, reader)
			if test.wantReason == "" {
				if len(got) != 0 {
					t.Fatalf("marker-loss counter = %v, want silence", got)
				}
				return
			}
			want := map[markerLossKey]int64{
				{failureClass: test.intent.Failure.FailureClass, reason: test.wantReason}: 1,
			}
			if len(got) != 1 || got[markerLossKey{failureClass: test.intent.Failure.FailureClass, reason: test.wantReason}] != 1 {
				t.Fatalf("marker-loss counter = %v, want exactly %v", got, want)
			}
		})
	}
}

// TestAdvisoryGroupHandlerWatermarkMarkerLossNilInstruments pins that the
// marker-loss path records nothing and never panics when the handler runs
// without instruments, matching every sibling Record* call.
func TestAdvisoryGroupHandlerWatermarkMarkerLossNilInstruments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	handler := Handler{
		Winners: &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(-time.Hour)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return markerLossTestNow },
	}
	_, err := handler.Handle(ctx, markerLossIntent(WinnersWatermarkNotReadyFailureClass, "no marker here"))
	if err == nil {
		t.Fatal("want a deferral error behind the watermark")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("failure class = %v, want %q", err, WinnersWatermarkNotReadyFailureClass)
	}
}

// TestAdvisoryGroupHandlerWatermarkStaleBoundaryExact pins the two sides
// of the 30-minute bound (#11089): a deferral of exactly
// winnersWatermarkMaxWait is not stale (the comparison is strict), and
// one nanosecond past it is stale.
func TestAdvisoryGroupHandlerWatermarkStaleBoundaryExact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cycleStart := handlerTestEnqueuedAt
	firstDeferred := cycleStart

	tests := []struct {
		name      string
		now       time.Time
		wantClass string
		wantWait  bool
	}{
		{
			name:      "exactly at the bound is not stale",
			now:       firstDeferred.Add(winnersWatermarkMaxWait),
			wantClass: WinnersWatermarkNotReadyFailureClass,
			wantWait:  true,
		},
		{
			name:      "one nanosecond past the bound is stale",
			now:       firstDeferred.Add(winnersWatermarkMaxWait + time.Nanosecond),
			wantClass: WinnersWatermarkStaleFailureClass,
			wantWait:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			intent := markerLossIntent(WinnersWatermarkNotReadyFailureClass, markerDetails(firstDeferred, cycleStart))
			handler := Handler{
				Winners:     &fakeWinnersReader{watermark: cycleStart.Add(-time.Hour)},
				Writer:      &fakeWriter{},
				Instruments: inst,
				Now:         func() time.Time { return test.now },
			}
			_, handleErr := handler.Handle(ctx, intent)
			if handleErr == nil {
				t.Fatal("want a deferral error behind the watermark")
			}
			if got := reducercontract.IsExpectedWait(handleErr); got != test.wantWait {
				t.Fatalf("IsExpectedWait = %v, want %v for %v", got, test.wantWait, handleErr)
			}
			var classified interface{ FailureClass() string }
			if !errors.As(handleErr, &classified) || classified.FailureClass() != test.wantClass {
				t.Fatalf("failure class = %v, want %q", handleErr, test.wantClass)
			}
			if got := collectMarkerLoss(t, reader); len(got) != 0 {
				t.Fatalf("healthy marker must stay silent at the boundary, got %v", got)
			}
		})
	}
}
