// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestPendingNVDWindowClaimsEvictsOldestAtCap pins the stash bound
// (eshu-hq/eshu#11287 review): at maxPendingNVDWindowClaims entries the
// stash evicts the entry with the oldest attempt time, so the map never
// grows without bound and the evicted window is left uncounted, never
// miscounted.
func TestPendingNVDWindowClaimsEvictsOldestAtCap(t *testing.T) {
	t.Parallel()

	base := testObservedAt()
	stash := func(p *pendingNVDWindowClaims, i int, at time.Time) {
		item := testWorkItem()
		item.WorkItemID = fmt.Sprintf("work-nvd-cap-%d", i)
		item.GenerationID = "generation-cap"
		p.stash(item, nvdWindowPending{generationID: "generation-cap", attemptedAt: at})
	}

	p := newPendingNVDWindowClaims()
	for i := 0; i < maxPendingNVDWindowClaims; i++ {
		stash(p, i, base.Add(time.Duration(i)*time.Second))
	}
	if got := len(p.rows); got != maxPendingNVDWindowClaims {
		t.Fatalf("len(rows) after fill = %d, want %d", got, maxPendingNVDWindowClaims)
	}

	stash(p, maxPendingNVDWindowClaims, base.Add(time.Duration(maxPendingNVDWindowClaims)*time.Second))
	if got := len(p.rows); got != maxPendingNVDWindowClaims {
		t.Fatalf("len(rows) after overflow = %d, want cap %d", got, maxPendingNVDWindowClaims)
	}
	oldest := testWorkItem()
	oldest.WorkItemID = "work-nvd-cap-0"
	oldest.GenerationID = "generation-cap"
	if _, ok := p.take(oldest); ok {
		t.Fatalf("oldest entry survived the cap eviction")
	}
	newest := testWorkItem()
	newest.WorkItemID = fmt.Sprintf("work-nvd-cap-%d", maxPendingNVDWindowClaims)
	newest.GenerationID = "generation-cap"
	if _, ok := p.take(newest); !ok {
		t.Fatalf("newest entry missing after the cap eviction")
	}
}

// TestNVDWindowTransportReadsStampedPayload pins the satellite-to-host
// transport contract (eshu-hq/eshu#11287): the satellite stamps
// nvd_window_transport into the source snapshot payload, the host folds it
// onto the closed transport set, and an absent or non-string stamp reads as
// unknown, never as feed.
func TestNVDWindowTransportReadsStampedPayload(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "feed", payload: map[string]any{nvdWindowTransportPayloadKey: "feed"}, want: telemetry.NVDTransportFeed},
		{name: "api", payload: map[string]any{nvdWindowTransportPayloadKey: "api"}, want: telemetry.NVDTransportAPI},
		{name: "absent reads unknown", payload: map[string]any{}, want: telemetry.NVDTransportUnknown},
		{name: "non-string reads unknown", payload: map[string]any{nvdWindowTransportPayloadKey: 7}, want: telemetry.NVDTransportUnknown},
		{name: "case variant reads unknown", payload: map[string]any{nvdWindowTransportPayloadKey: "FEED"}, want: telemetry.NVDTransportUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nvdWindowTransport(tc.payload); got != tc.want {
				t.Fatalf("nvdWindowTransport(%v) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

// TestNVDWindowClaimCountsTransportsSeparately is the failing-first proof
// for #11287: a feed-served window and an API-served window count under
// different transport labels through the production host emitter.
func TestNVDWindowClaimCountsTransportsSeparately(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-nvd-window",
		Config:              map[string]any{},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Instruments:         inst,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	ctx := context.Background()
	source.recordNVDWindowClaim(ctx, telemetry.NVDTransportFeed, telemetry.NVDWindowClaimCommitted, 12.5, true)
	source.recordNVDWindowClaim(ctx, telemetry.NVDTransportAPI, telemetry.NVDWindowClaimCommitted, 7.5, false)

	byTransport := map[string]int64{}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_vulnerability_nvd_window_claims_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("claims data = %#v, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				transport, _ := point.Attributes.Value("transport")
				byTransport[transport.AsString()] += point.Value
			}
		}
	}
	if byTransport["feed"] != 1 || byTransport["api"] != 1 {
		t.Fatalf("claims by transport = %v, want exactly one feed and one api series", byTransport)
	}
}
