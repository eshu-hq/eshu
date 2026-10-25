// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
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
	if got, want := []string{ContentFileUnclassifiedSampledOut, ContentFileUnclassifiedClassifyFailed}, []string{"sampled_out", "classify_failed"}; !slices.Equal(got, want) {
		t.Fatalf("content file unclassified reasons = %v, want the closed set %v", got, want)
	}
	if got, want := []string{ContentFileBatchSmall, ContentFileBatchLarge}, []string{"small", "large"}; !slices.Equal(got, want) {
		t.Fatalf("content file batch classes = %v, want the closed set %v", got, want)
	}
	if got, want := MetricDimensionBatchClass, "batch_class"; got != want {
		t.Fatalf("MetricDimensionBatchClass = %q, want %q", got, want)
	}
	if !slices.Contains(MetricDimensionKeys(), "batch_class") {
		t.Fatal("batch_class is not in the frozen MetricDimensionKeys registry")
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
	if instruments.ContentFileUpsertUnclassifiedBodyBytes == nil {
		t.Fatal("ContentFileUpsertUnclassifiedBodyBytes counter not registered")
	}
	if instruments.ContentFileUpsertUnclassifiedRows == nil {
		t.Fatal("ContentFileUpsertUnclassifiedRows counter not registered")
	}
}

// TestContentFileUpsertScrapeNames pins the Prometheus series names operators
// query (#8950): the exporter must not rewrite either counter name.
func TestContentFileUpsertScrapeNames(t *testing.T) {
	providers := newTestProviders(t)
	instruments, err := NewInstruments(providers.MeterProvider.Meter("eshu/content-file-upserts-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	ctx := context.Background()
	instruments.ContentFileUpserts.Add(ctx, 3, metric.WithAttributes(
		AttrOutcome(ContentFileUpsertOutcomeMetadataOnly), AttrSnapshot(ContentFileSnapshotFull), AttrBatchClass(ContentFileBatchLarge)))
	instruments.ContentFileUpsertBodyBytes.Add(ctx, 1024, metric.WithAttributes(
		AttrOutcome(ContentFileUpsertOutcomeMetadataOnly), AttrSnapshot(ContentFileSnapshotFull), AttrBatchClass(ContentFileBatchLarge)))
	instruments.ContentFileUpsertUnclassifiedBodyBytes.Add(ctx, 2048, metric.WithAttributes(
		AttrReason(ContentFileUnclassifiedSampledOut), AttrSnapshot(ContentFileSnapshotDelta), AttrBatchClass(ContentFileBatchSmall)))

	instruments.ContentFileUpsertUnclassifiedRows.Add(ctx, 7, metric.WithAttributes(
		AttrReason(ContentFileUnclassifiedSampledOut), AttrSnapshot(ContentFileSnapshotDelta), AttrBatchClass(ContentFileBatchSmall)))

	code, body := scrapeMetrics(providers.PrometheusHandler)
	if code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200: %s", code, body)
	}
	for _, want := range []string{
		`eshu_dp_content_file_upserts_total{`,
		`outcome="metadata_only"`,
		`snapshot="full"`,
		`eshu_dp_content_file_upsert_body_bytes_total{`,
		`eshu_dp_content_file_upsert_unclassified_rows_total{`,
		`eshu_dp_content_file_upsert_unclassified_body_bytes_total{`,
		`reason="sampled_out"`,
		`batch_class="small"`,
		`batch_class="large"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q", want)
		}
	}
}
