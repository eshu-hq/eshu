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

func collectOutcomeMetrics(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
},
) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return rm
}

// TestContentWriterSamplesSmallBatches pins the sampling policy: the pre-read
// has a fixed per-batch cost, so batches under the minimum size are classified
// one in every N, and every skipped row and its body bytes are still counted as
// unclassified, labelled small, so the counters add up to the rows written.
func TestContentWriterSamplesSmallBatches(t *testing.T) {
	t.Parallel()

	writer, fake, reader, logs := newOutcomeWriter(t, nil)
	writer = writer.WithOutcomeSampling(contentFileOutcomeMinBatchRows, contentFileOutcomeSampleEvery)
	const writes = 160
	for i := range writes {
		writeFiles(t, writer, fmt.Sprintf("g%d", i), 1)
	}
	classified := preReadCount(fake)
	if want := writes / contentFileOutcomeSampleEvery; classified < want-3 || classified > want+3 {
		t.Fatalf("pre-reads for %d one-file writes = %d, want about %d (one in %d)", writes, classified, want, contentFileOutcomeSampleEvery)
	}
	rm := collectOutcomeMetrics(t, reader)
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"batch_class": "small"}); got != int64(classified) {
		t.Errorf("classified small rows = %d, want %d", got, classified)
	}
	skipped := int64(writes - classified)
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_rows_total", map[string]string{"reason": "sampled_out", "snapshot": "delta", "batch_class": "small"}); got != skipped {
		t.Errorf("sampled_out rows = %d, want %d", got, skipped)
	}
	if got, want := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_body_bytes_total", map[string]string{"reason": "sampled_out", "snapshot": "delta", "batch_class": "small"}), skipped*int64(len("package a\n")); got != want {
		t.Errorf("sampled_out body bytes = %d, want %d", got, want)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_rows_total", map[string]string{"batch_class": "large"}); got != 0 {
		t.Errorf("large-batch unclassified rows = %d, want 0", got)
	}
	if line := logLineForStage(t, logs.String(), "upsert_files"); line["sampled_out_batch_count"] == nil {
		t.Errorf("upsert_files log lacks sampled_out_batch_count: %v", line)
	}
}

// TestNewContentWriterSamplesByDefault pins that the shipped constructor turns
// sampling on. Every other sampling test sets the policy itself, so deleting the
// default in NewContentWriter would otherwise pass them all.
func TestNewContentWriterSamplesByDefault(t *testing.T) {
	t.Parallel()

	_, fake, _, _ := newOutcomeWriter(t, nil)
	// Build the writer the way production wiring does: the constructor and the
	// instruments, nothing else.
	reader, instruments := newOutcomeInstruments(t)
	writer := NewContentWriter(fake).WithInstruments(instruments)
	const writes = 160
	for i := range writes {
		writeFiles(t, writer, fmt.Sprintf("g%d", i), 1)
	}
	classified := preReadCount(fake)
	if want := writes / contentFileOutcomeSampleEvery; classified < want-3 || classified > want+3 {
		t.Fatalf("pre-reads for %d one-file writes through NewContentWriter = %d, want about %d: the default sampling policy is off", writes, classified, want)
	}
	rm := collectOutcomeMetrics(t, reader)
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_rows_total", map[string]string{"reason": "sampled_out"}); got != int64(writes-classified) {
		t.Errorf("sampled_out rows = %d, want %d", got, writes-classified)
	}
}

// TestContentWriterAlwaysClassifiesLargeBatches pins that a batch at or above
// the minimum size is classified whatever the sampler's position, is labelled
// large, and does not spend a sampler slot.
func TestContentWriterAlwaysClassifiesLargeBatches(t *testing.T) {
	t.Parallel()

	writer, fake, reader, _ := newOutcomeWriter(t, nil)
	writer = writer.WithOutcomeSampling(contentFileOutcomeMinBatchRows, contentFileOutcomeSampleEvery)
	writeFiles(t, writer, "g0", 1) // takes the first sampler slot, which classifies
	writeFiles(t, writer, "g1", 1) // the second slot is skipped
	for i := range 3 {
		writeFiles(t, writer, fmt.Sprintf("big%d", i), contentFileOutcomeMinBatchRows)
	}
	if got := preReadCount(fake); got != 4 {
		t.Fatalf("pre-reads = %d, want 4 (one sampled small batch plus three large)", got)
	}
	rm := collectOutcomeMetrics(t, reader)
	if got, want := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"batch_class": "large"}), int64(3*contentFileOutcomeMinBatchRows); got != want {
		t.Errorf("large classified rows = %d, want %d", got, want)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"batch_class": "small"}); got != 1 {
		t.Errorf("small classified rows = %d, want 1", got)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_body_bytes_total", map[string]string{"batch_class": "large", "snapshot": "delta"}); got != int64(3*contentFileOutcomeMinBatchRows*len("package a\n")) {
		t.Errorf("large body bytes = %d", got)
	}
}

// TestOutcomeSamplerRateAndAliasing pins the sampler's long-run rate and that it
// does not alias with a periodic workload. A workload that cycles a fixed list of
// n repositories, one small batch each, reaches the sampler with period n; a
// modulo sampler would classify the same positions of that cycle every time.
// Each phase of every tested period must be hit at about one in N.
func TestOutcomeSamplerRateAndAliasing(t *testing.T) {
	t.Parallel()

	const (
		every = contentFileOutcomeSampleEvery
		calls = 80000
	)
	sampler := &outcomeSampler{minRows: contentFileOutcomeMinBatchRows, every: every}
	hits := 0
	byPhase := map[int][]int{}
	periods := []int{2, 4, 8, 16, 24, 64}
	for _, p := range periods {
		byPhase[p] = make([]int, p)
	}
	for i := range calls {
		if sampler.classify(1) {
			hits++
			for _, p := range periods {
				byPhase[p][i%p]++
			}
		}
	}
	if want := calls / every; hits < want*98/100 || hits > want*102/100 {
		t.Fatalf("long-run classified %d of %d small batches, want about %d (one in %d)", hits, calls, want, every)
	}
	for _, p := range periods {
		want := calls / p / every
		for phase, got := range byPhase[p] {
			if got < want*93/100 || got > want*107/100 {
				t.Errorf("period %d phase %d classified %d times, want about %d: the sampler aliases with the workload", p, phase, got, want)
			}
		}
	}
	// A large batch is always classified and spends no slot.
	before := sampler.next.Load()
	if !sampler.classify(contentFileOutcomeMinBatchRows) || sampler.next.Load() != before {
		t.Fatal("a large batch must classify without spending a sampler slot")
	}
}
