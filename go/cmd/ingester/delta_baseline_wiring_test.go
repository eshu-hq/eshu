// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"log/slog"
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
// regression for the missing delta-union counters and the union-decision log
// line: the in-process projector must hand its telemetry instruments and its
// logger to the Postgres fact store, the way cmd/projector does, or
// recordDeltaUnion skips them and the operator exposition stays silent.
func TestBuildIngesterProjectorServicePassesInstrumentsToFactStore(t *testing.T) {
	t.Parallel()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := buildIngesterProjectorService(queryErrorDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, instruments, logger)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if service.FactStore == nil {
		t.Fatal("FactStore = nil, want the Postgres fact store")
	}
	carrier, ok := service.FactStore.(interface {
		Instruments() *telemetry.Instruments
		Logger() *slog.Logger
	})
	if !ok {
		t.Fatalf("FactStore type = %T, want the instruments and logger accessors", service.FactStore)
	}
	if got := carrier.Instruments(); got != instruments {
		t.Fatalf("FactStore instruments = %v, want the wiring instruments", got)
	}
	if got := carrier.Logger(); got != logger {
		t.Fatalf("FactStore logger = %v, want the wiring logger", got)
	}
}
