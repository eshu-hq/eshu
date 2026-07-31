// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestContainerImageIdentityRetirementCountersUseBoundedOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := ContainerImageIdentityHandler{Instruments: instruments}
	handler.emitRetirementCounters(
		context.Background(),
		ContainerImageIdentityWriteResult{
			RetirementAttempts: 2,
			LegacyRowsDeleted:  1,
		},
		map[string]int{
			containerImageIdentityRetireHoldTagListTruncated: 3,
		},
	)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, outcome := range []string{
		"retirement_attempted",
		"legacy_deleted",
		"held_tag_list_truncated",
	} {
		if !metricHasAttrs(
			metrics,
			"eshu_dp_container_image_identity_retirements_total",
			map[string]string{
				telemetry.MetricDimensionDomain:  string(DomainContainerImageIdentity),
				telemetry.MetricDimensionOutcome: outcome,
			},
		) {
			t.Fatalf("retirement counter outcome %q not emitted", outcome)
		}
	}
}
