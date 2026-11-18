// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestSupplyChainImpactWinnersWriteConflictContract pins the closed site set
// of eshu_dp_supply_chain_impact_winners_write_conflicts_total (#10851) and
// that NewInstruments registers the counter.
func TestSupplyChainImpactWinnersWriteConflictContract(t *testing.T) {
	t.Parallel()

	gotSites := []string{
		SupplyChainImpactWinnersConflictSiteWriterCapture,
		SupplyChainImpactWinnersConflictSiteWriterCommit,
		SupplyChainImpactWinnersConflictSiteTick,
	}
	wantSites := []string{"writer_capture", "writer_commit", "tick"}
	if !slices.Equal(gotSites, wantSites) {
		t.Fatalf("write-conflict sites = %v, want the closed set %v", gotSites, wantSites)
	}
	if got, want := MetricDimensionSite, "site"; got != want {
		t.Fatalf("MetricDimensionSite = %q, want %q", got, want)
	}
	if !slices.Contains(MetricDimensionKeys(), MetricDimensionSite) {
		t.Fatal("site is not in the frozen MetricDimensionKeys registry")
	}
	if got, want := MetricDimensionFailureClass, "failure_class"; got != want {
		t.Fatalf("MetricDimensionFailureClass = %q, want %q", got, want)
	}
	if !slices.Contains(MetricDimensionKeys(), MetricDimensionFailureClass) {
		t.Fatal("failure_class is not in the frozen MetricDimensionKeys registry")
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.SupplyChainImpactWinnersWriteConflicts == nil {
		t.Fatal("SupplyChainImpactWinnersWriteConflicts counter not registered")
	}
}

// TestSupplyChainImpactWinnersWriteConflictScrapeNames pins the Prometheus
// series names operators query (#10851): the exporter must not rewrite the
// counter name, and both label dimensions must survive the scrape.
func TestSupplyChainImpactWinnersWriteConflictScrapeNames(t *testing.T) {
	providers := newTestProviders(t)
	instruments, err := NewInstruments(providers.MeterProvider.Meter("eshu/winners-write-conflict-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	ctx := context.Background()
	instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx,
		SupplyChainImpactWinnersConflictSiteTick, "deadlock")
	instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx,
		SupplyChainImpactWinnersConflictSiteWriterCommit, "serialization_failure")
	// An empty class records nothing: a non-conflict error leaves the
	// counter flat.
	instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx,
		SupplyChainImpactWinnersConflictSiteWriterCapture, "")
	// An unknown site records nothing: a typo'd caller must not open a
	// new series.
	instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx,
		"writer_captrue", "deadlock")

	code, body := scrapeMetrics(providers.PrometheusHandler)
	if code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200: %s", code, body)
	}
	for _, want := range []string{
		`eshu_dp_supply_chain_impact_winners_write_conflicts_total{`,
		`site="tick"`,
		`site="writer_commit"`,
		`failure_class="deadlock"`,
		`failure_class="serialization_failure"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q", want)
		}
	}
	if strings.Contains(body, `site="writer_capture"`) {
		t.Error("scrape contains a writer_capture point, but the empty-class record must add nothing")
	}
	if strings.Contains(body, `site="writer_captrue"`) {
		t.Error("scrape contains a writer_captrue point, but an unknown site must add nothing")
	}
}
