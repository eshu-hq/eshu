// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type generationOutcomeKey struct{ collectorKind, outcome, reason string }

// generationOutcomes reads eshu_dp_ingestion_generation_outcomes_total from a
// manual reader, keyed by collector_kind, outcome, and reason (#10020).
func generationOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[generationOutcomeKey]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[generationOutcomeKey]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_ingestion_generation_outcomes_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("outcomes counter data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				key := generationOutcomeKey{}
				for _, kv := range point.Attributes.ToSlice() {
					switch string(kv.Key) {
					case telemetry.MetricDimensionCollectorKind:
						key.collectorKind = kv.Value.AsString()
					case telemetry.MetricDimensionOutcome:
						key.outcome = kv.Value.AsString()
					case telemetry.MetricDimensionReason:
						key.reason = kv.Value.AsString()
					default:
						t.Fatalf("unexpected label %q on generation outcomes counter", kv.Key)
					}
				}
				out[key] += point.Value
			}
		}
	}
	return out
}

func requireGenerationOutcomes(t *testing.T, got, want map[generationOutcomeKey]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("generation outcomes = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("generation outcomes[%+v] = %d, want %d (all: %v)", key, got[key], value, got)
		}
	}
}

// TestCommitMintedGenerationCountsMintedOutcome proves a stored generation
// moves the minted series, so an operator can count mints per collector kind.
func TestCommitMintedGenerationCountsMintedOutcome(t *testing.T) {
	database := newGuardRoutingDB(t, 10, 10, 10)
	scopeValue, generation := guardDeltaFixture(false)
	instruments, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(database)
	store.Instruments = instruments

	stream := guardStream(generation, false, nil, nil, nil, nil, 10)
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	requireStored(t, database)
	requireGenerationOutcomes(t, generationOutcomes(t, reader), map[generationOutcomeKey]int64{
		{"git", telemetry.GenerationOutcomeMinted, telemetry.GenerationReasonNone}: 1,
	})
}

// TestCommitUnchangedGenerationCountsSkippedOutcome proves the unchanged skip
// moves the skipped series and not the minted one.
func TestCommitUnchangedGenerationCountsSkippedOutcome(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	database := &fakeTransactionalDB{tx: &fakeTx{}, queryResponses: []queueFakeRows{{
		rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}},
	}}}
	scopeValue, generation := reprojectFixture("", "hint-same", "sha-same")
	instruments, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(database)
	store.Instruments = instruments

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, threeFactStream()); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	requireGenerationOutcomes(t, generationOutcomes(t, reader), map[generationOutcomeKey]int64{
		{"git", telemetry.GenerationOutcomeSkipped, telemetry.RefreshSkipReasonUnchanged}: 1,
	})
}

// TestCommitRefusedSuccessorCountsSkippedNotMinted proves a refused
// destructive successor is a skipped outcome with its own reason, never a mint.
func TestCommitRefusedSuccessorCountsSkippedNotMinted(t *testing.T) {
	database := newGuardRoutingDB(t, 100, 100, 100)
	scopeValue, generation := guardDeltaFixture(false)
	instruments, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(database)
	store.Instruments = instruments

	stream := guardStream(generation, false, nil, nil, nil, nil, 1)
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	requireGenerationOutcomes(t, generationOutcomes(t, reader), map[generationOutcomeKey]int64{
		{"git", telemetry.GenerationOutcomeSkipped, telemetry.RefreshSkipReasonDestructiveSuccessor}: 1,
	})
}

// failCommitTx wraps a transaction so Commit fails, as a lost connection or a
// deferred constraint would.
type failCommitTx struct{ db.Transaction }

func (failCommitTx) Commit() error { return errors.New("commit failed") }

// failCommitDB hands out failCommitTx transactions.
type failCommitDB struct{ *guardRoutingDB }

func (f failCommitDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := f.guardRoutingDB.Begin(ctx)
	return failCommitTx{tx}, err
}

// TestCommitFailedTransactionCountsNoOutcome proves a generation whose commit
// fails is counted as neither minted nor skipped, so the minted series never
// includes a rolled-back generation.
func TestCommitFailedTransactionCountsNoOutcome(t *testing.T) {
	database := newGuardRoutingDB(t, 10, 10, 10)
	scopeValue, generation := guardDeltaFixture(false)
	instruments, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(failCommitDB{database})
	store.Instruments = instruments

	stream := guardStream(generation, false, nil, nil, nil, nil, 10)
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err == nil {
		t.Fatal("CommitScopeGeneration() error = nil, want the commit failure")
	}
	requireGenerationOutcomes(t, generationOutcomes(t, reader), map[generationOutcomeKey]int64{})
}
