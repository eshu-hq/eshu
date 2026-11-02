// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestBuildIngesterProjectorServiceWiresDeltaBaselineFence fails when the
// ingester binary builds its projector Service without the #7319 fence.
func TestBuildIngesterProjectorServiceWiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	service, err := buildIngesterProjectorService(queryErrorDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if service.DeltaBaselineFence == nil {
		t.Fatal("DeltaBaselineFence = nil, want the projector queue")
	}
	if _, ok := service.DeltaBaselineFence.(postgres.ProjectorQueue); !ok {
		t.Fatalf("DeltaBaselineFence type = %T, want postgres.ProjectorQueue", service.DeltaBaselineFence)
	}
}

// TestBuildIngesterProjectorServiceWiresWriteMarker fails when the binary builds its projector Service
// without the #7389 projection write-start marker.
func TestBuildIngesterProjectorServiceWiresWriteMarker(t *testing.T) {
	t.Parallel()
	service, err := buildIngesterProjectorService(queryErrorDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if _, ok := service.WriteMarker.(postgres.ProjectorQueue); !ok {
		t.Fatalf("WriteMarker type = %T, want postgres.ProjectorQueue", service.WriteMarker)
	}
}

// TestBuildIngesterProjectorServicePassesInstrumentsToFactStore is the #8785
// regression for the missing delta-union counters: the in-process projector
// must hand its telemetry instruments to the Postgres fact store, or
// recordDeltaUnion skips them and the operator exposition stays silent.
func TestBuildIngesterProjectorServicePassesInstrumentsToFactStore(t *testing.T) {
	t.Parallel()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	service, err := buildIngesterProjectorService(queryErrorDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, instruments, nil)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if service.FactStore == nil {
		t.Fatal("FactStore = nil, want the Postgres fact store")
	}
	carrier, ok := service.FactStore.(interface {
		Instruments() *telemetry.Instruments
	})
	if !ok {
		t.Fatalf("FactStore type = %T, want the instruments accessor", service.FactStore)
	}
	if got := carrier.Instruments(); got != instruments {
		t.Fatalf("FactStore instruments = %v, want the wiring instruments", got)
	}
}
