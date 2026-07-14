// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRepoDependencyProjectionRunnerRecordsLeaseQuarantineCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	runner := RepoDependencyProjectionRunner{Instruments: instruments}
	runner.recordRepoDependencyCycleFailure(
		context.Background(),
		&repoDependencyLeaseQuarantineError{delay: 5 * time.Minute, cause: context.DeadlineExceeded},
		1,
	)

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !quarantineCounterHasPoint(resources, "cycle_deadline", 1) {
		t.Fatal("lease quarantine counter is missing domain/reason point")
	}
}

func quarantineCounterHasPoint(resources metricdata.ResourceMetrics, reason string, want int64) bool {
	for _, scope := range resources.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "eshu_dp_shared_projection_lease_quarantines_total" {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				domain, domainOK := point.Attributes.Value(attribute.Key("domain"))
				gotReason, reasonOK := point.Attributes.Value(attribute.Key("reason"))
				if domainOK && reasonOK && domain.AsString() == DomainRepoDependency &&
					gotReason.AsString() == reason && point.Value == want {
					return true
				}
			}
		}
	}
	return false
}
