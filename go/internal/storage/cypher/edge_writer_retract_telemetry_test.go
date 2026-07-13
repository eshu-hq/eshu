// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestEdgeWriterCodeImportRetractRecordsOnlyExactSourceOmissionTelemetry(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := NewEdgeWriter(&recordingExecutor{}, 0)
	writer.Instruments = instruments
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      map[string]any{"repo_id": "repo-a"},
	}}
	if err := writer.RetractEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		rows,
		codeImportRepoDependencyEvidenceSource,
	); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	for _, evidenceSource := range []string{"resolver/cross-repo", "projection/code-imports-extra"} {
		if err := writer.RetractEdges(
			context.Background(),
			reducer.DomainRepoDependency,
			rows,
			evidenceSource,
		); err != nil {
			t.Fatalf("RetractEdges(%q) error = %v", evidenceSource, err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertInt64CounterValue(t, rm, "eshu_dp_shared_edge_runs_on_retract_omissions_total", map[string]string{
		"domain": reducer.DomainRepoDependency,
		"reason": "source_capability",
	}, 1)
}
