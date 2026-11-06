// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// refusingCommitter drains the fact stream, marks the commit refused on the
// disposition box, and returns nil — the production refusal shape: the
// committer rolled the generation back but the claim may complete (#10037).
type refusingCommitter struct {
	fakeCommitter
}

func (c *refusingCommitter) CommitScopeGeneration(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if factStream != nil {
		for range factStream {
		}
	}
	collector.MarkCommitRefused(ctx, collector.CommitRefusal{
		Reason: "destructive_successor_refused",
		Rule:   "collector_ratio",
	})
	return nil
}

// factsCommittedTotal sums eshu_dp_facts_committed_total across all attribute
// sets. It returns 0 when the counter was never recorded.
func factsCommittedTotal(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	const name = "eshu_dp_facts_committed_total"
	var total int64
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Sum[int64]", name, m.Data)
			}
			found = true
			for _, p := range sum.DataPoints {
				total += p.Value
			}
		}
	}
	if !found {
		return 0
	}
	return total
}

func refusedCommitTestGeneration() collector.CollectedGeneration {
	return collector.FactsFromSlice(
		scope.IngestionScope{
			ScopeID:       "scope-refused",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
		},
		scope.ScopeGeneration{
			GenerationID: "gen-refused",
			ScopeID:      "scope-refused",
		},
		[]facts.Envelope{{}, {}, {}},
	)
}

func newCommittedInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-refused-commit-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return instruments, reader
}

// TestBootstrapRefusedCommitCountsNoStoredTotals pins #10085: a refused
// commit is rolled back before anything persists, so it adds nothing to the
// committed-facts counter and nothing to the bootstrap run totals.
func TestBootstrapRefusedCommitCountsNoStoredTotals(t *testing.T) {
	t.Parallel()

	instruments, reader := newCommittedInstruments(t)
	state := &collectorDrainState{
		committer:   &refusingCommitter{},
		instruments: instruments,
	}

	if err := state.commitCollectedGeneration(
		context.Background(), refusedCommitTestGeneration(), nil, time.Now(),
	); err != nil {
		t.Fatalf("commitCollectedGeneration() error = %v, want nil (a refusal is not a commit failure)", err)
	}

	if got := state.total.Load(); got != 0 {
		t.Errorf("scopes collected = %d, want 0 (a refused generation is not stored)", got)
	}
	if got := state.totalFacts.Load(); got != 0 {
		t.Errorf("total facts = %d, want 0 (a refused generation is rolled back)", got)
	}
	if got := state.totalEntities.Load(); got != 0 {
		t.Errorf("total entities = %d, want 0 (a refused generation is rolled back)", got)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := factsCommittedTotal(t, rm); got != 0 {
		t.Errorf("eshu_dp_facts_committed_total = %d, want 0 (a refused commit stores nothing)", got)
	}
}

// TestBootstrapCommittedCommitCountsStoredTotals is the control for the
// refusal test above: a stored generation still counts its facts in the
// committed-facts counter and the bootstrap run totals.
func TestBootstrapCommittedCommitCountsStoredTotals(t *testing.T) {
	t.Parallel()

	instruments, reader := newCommittedInstruments(t)
	state := &collectorDrainState{
		committer:   &fakeCommitter{},
		instruments: instruments,
	}

	if err := state.commitCollectedGeneration(
		context.Background(), refusedCommitTestGeneration(), nil, time.Now(),
	); err != nil {
		t.Fatalf("commitCollectedGeneration() error = %v, want nil", err)
	}

	if got := state.total.Load(); got != 1 {
		t.Errorf("scopes collected = %d, want 1", got)
	}
	if got := state.totalFacts.Load(); got != 3 {
		t.Errorf("total facts = %d, want 3", got)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := factsCommittedTotal(t, rm); got != 3 {
		t.Errorf("eshu_dp_facts_committed_total = %d, want 3", got)
	}
}

// BenchmarkCommitCollectedGenerationStored measures the Go-side cost of the
// stored commit path per collected generation (fake in-memory committer, 3
// facts, no logger or span). It pins the #10085 no-regression claim: the
// disposition box and the refusal read add only a context wrap and a branch
// to this path, while a refused generation does strictly less work.
func BenchmarkCommitCollectedGenerationStored(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	state := &collectorDrainState{
		committer:   &fakeCommitter{},
		instruments: instruments,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := state.commitCollectedGeneration(
			context.Background(), refusedCommitTestGeneration(), nil, time.Now(),
		); err != nil {
			b.Fatalf("commitCollectedGeneration() error = %v, want nil", err)
		}
	}
}
