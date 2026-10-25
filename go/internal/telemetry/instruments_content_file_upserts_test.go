// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestContentFileUpsertContract pins the closed outcome and snapshot value
// sets of eshu_dp_content_file_upserts_total (#8950) and that NewInstruments
// registers both counters.
func TestContentFileUpsertContract(t *testing.T) {
	t.Parallel()

	wantOutcomes := []string{"inserted", "unchanged_skipped", "metadata_only", "content_changed"}
	gotOutcomes := []string{
		ContentFileUpsertOutcomeInserted,
		ContentFileUpsertOutcomeUnchangedSkipped,
		ContentFileUpsertOutcomeMetadataOnly,
		ContentFileUpsertOutcomeContentChanged,
	}
	if !slices.Equal(gotOutcomes, wantOutcomes) {
		t.Fatalf("content file upsert outcomes = %v, want the closed set %v", gotOutcomes, wantOutcomes)
	}
	if got, want := []string{ContentFileSnapshotFull, ContentFileSnapshotDelta}, []string{"full", "delta"}; !slices.Equal(got, want) {
		t.Fatalf("content file snapshot values = %v, want the closed set %v", got, want)
	}
	if got, want := MetricDimensionSnapshot, "snapshot"; got != want {
		t.Fatalf("MetricDimensionSnapshot = %q, want %q", got, want)
	}
	if !slices.Contains(MetricDimensionKeys(), "snapshot") {
		t.Fatal("snapshot is not in the frozen MetricDimensionKeys registry")
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ContentFileUpserts == nil {
		t.Fatal("ContentFileUpserts counter not registered")
	}
	if instruments.ContentFileUpsertBodyBytes == nil {
		t.Fatal("ContentFileUpsertBodyBytes counter not registered")
	}
}
