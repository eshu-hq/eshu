// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func preReadCount(fake *outcomeFakeDB) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	n := 0
	for _, q := range fake.queries {
		if q.query == contentFileOutcomePreReadSQL {
			n++
		}
	}
	return n
}

func writeFiles(t *testing.T, writer ContentWriter, gen string, files int) {
	t.Helper()
	records := make([]content.Record, files)
	for i := range records {
		records[i] = content.Record{Path: fmt.Sprintf("f%03d.go", i), Body: "package a\n"}
	}
	if _, err := writer.Write(context.Background(), content.Materialization{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: gen, Records: records}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

// TestContentWriterSamplesSmallBatches pins the default sampling policy: the
// pre-read has a fixed per-batch cost, so batches under the minimum size are
// classified one in every N, and every skipped row is still counted as
// unclassified so the two counters add up to the rows written.
func TestContentWriterSamplesSmallBatches(t *testing.T) {
	t.Parallel()

	writer, fake, reader, logs := newOutcomeWriter(t, nil)
	writer = writer.WithOutcomeSampling(contentFileOutcomeMinBatchRows, contentFileOutcomeSampleEvery)
	const writes = contentFileOutcomeSampleEvery
	for i := range writes {
		writeFiles(t, writer, fmt.Sprintf("g%d", i), 1)
	}
	if got := preReadCount(fake); got != 1 {
		t.Fatalf("pre-reads for %d one-file writes = %d, want 1 (one in %d)", writes, got, contentFileOutcomeSampleEvery)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", nil); got != 1 {
		t.Errorf("classified rows = %d, want 1", got)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_rows_total", map[string]string{"reason": "sampled_out", "snapshot": "delta"}); got != writes-1 {
		t.Errorf("sampled_out rows = %d, want %d", got, writes-1)
	}
	if line := logLineForStage(t, logs.String(), "upsert_files"); line["sampled_out_batch_count"] == nil {
		t.Errorf("upsert_files log lacks sampled_out_batch_count: %v", line)
	}
}

// TestContentWriterAlwaysClassifiesLargeBatches pins that a batch at or above
// the minimum size is classified whatever the sampler's position, and does not
// spend a sampler slot.
func TestContentWriterAlwaysClassifiesLargeBatches(t *testing.T) {
	t.Parallel()

	writer, fake, reader, _ := newOutcomeWriter(t, nil)
	writer = writer.WithOutcomeSampling(contentFileOutcomeMinBatchRows, contentFileOutcomeSampleEvery)
	writeFiles(t, writer, "g0", 1) // takes the first sampler slot
	writeFiles(t, writer, "g1", 1) // sampled out
	for i := range 3 {
		writeFiles(t, writer, fmt.Sprintf("big%d", i), contentFileOutcomeMinBatchRows)
	}
	if got := preReadCount(fake); got != 4 {
		t.Fatalf("pre-reads = %d, want 4 (one sampled small batch plus three large)", got)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got, want := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", nil), int64(1+3*contentFileOutcomeMinBatchRows); got != want {
		t.Errorf("classified rows = %d, want %d", got, want)
	}
}
