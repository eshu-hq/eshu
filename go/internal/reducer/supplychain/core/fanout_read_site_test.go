// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestFanOutReadTimeoutCarriesReadSite proves a failed holder read counts its
// read site (#10564 H), so the next regression names the read that timed
// out: the holder lookup reads "holders", the queue-state read "in_flight".
func TestFanOutReadTimeoutCarriesReadSite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		port *stubFanOut
		site string
	}{
		{"holders", &stubFanOut{findErr: ErrFanOutReadTimeout}, telemetry.FanOutReadSiteHolders},
		{"in-flight", &stubFanOut{inFlightErr: ErrFanOutReadTimeout}, telemetry.FanOutReadSiteInFlight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			if _, err := fanOutHandler(tc.port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err == nil {
				t.Fatal("Handle() error = nil, want the read timeout")
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			for _, scope := range rm.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
						continue
					}
					for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
						outcome, _ := dp.Attributes.Value("outcome")
						if outcome.AsString() != "read_timeout" {
							continue
						}
						site, ok := dp.Attributes.Value(telemetry.MetricDimensionSite)
						if !ok {
							t.Fatal("read_timeout datapoint carries no site label")
						}
						if site.AsString() != tc.site {
							t.Fatalf("read_site = %q, want %q", site.AsString(), tc.site)
						}
						return
					}
				}
			}
			t.Fatal("no read_timeout datapoint recorded")
		})
	}
}
