// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// staleWritesMetric is the #6679 advance-only guard skip counter.
const staleWritesMetric = "eshu_dp_shared_acceptance_stale_writes_total"

// newStaleWriteInstruments returns instruments backed by a manual reader.
func newStaleWriteInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("postgres-acceptance-stale"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// staleWritesByDomain collects the stale-write counter keyed by its domain
// label; an unrecorded counter yields an empty map.
func staleWritesByDomain(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	byDomain := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != staleWritesMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", staleWritesMetric, m.Data)
			}
			for _, dp := range sum.DataPoints {
				if dp.Attributes.Len() != 1 {
					t.Fatalf("%s attributes = %v, want only domain", staleWritesMetric, dp.Attributes.ToSlice())
				}
				domain, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				byDomain[domain.AsString()] += dp.Value
			}
		}
	}
	return byDomain
}

func staleWriteIntent(generationID string, at time.Time) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:         "intent-6679-" + generationID + "-" + at.Format(time.RFC3339Nano),
		ProjectionDomain: reducer.DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          "scope-6679",
		AcceptanceUnitID: "repository:6679",
		RepositoryID:     "repository:6679",
		SourceRunID:      "run-6679",
		GenerationID:     generationID,
		Payload: map[string]any{
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
		},
		CreatedAt: at,
	}
}

// TestSharedIntentAcceptanceWriterCountsStaleWritesFromReturning proves the
// writer counts exactly the submitted keys the upsert withheld from RETURNING
// and records nothing when every key applied.
func TestSharedIntentAcceptanceWriterCountsStaleWritesFromReturning(t *testing.T) {
	t.Parallel()

	instruments, reader := newStaleWriteInstruments(t)
	database := &codeCallIntentWriterTestDB{}
	writer := NewCodeCallIntentWriterWithInstruments(database, instruments)
	now := time.Now().UTC().Truncate(time.Microsecond)

	if err := writer.UpsertIntents(context.Background(), []reducer.SharedProjectionIntentRow{staleWriteIntent("gen-new", now)}); err != nil {
		t.Fatalf("UpsertIntents(applied) error = %v", err)
	}
	if got := staleWritesByDomain(t, reader); len(got) != 0 {
		t.Fatalf("stale writes after applied upsert = %v, want none", got)
	}

	database.staleKeys = map[string]struct{}{"scope-6679|repository:6679|run-6679": {}}
	if err := writer.UpsertIntents(context.Background(), []reducer.SharedProjectionIntentRow{staleWriteIntent("gen-old", now)}); err != nil {
		t.Fatalf("UpsertIntents(stale) error = %v", err)
	}
	got := staleWritesByDomain(t, reader)
	if got[reducer.DomainCodeCalls] != 1 || len(got) != 1 {
		t.Fatalf("stale writes = %v, want {%s: 1}", got, reducer.DomainCodeCalls)
	}
}

// TestBuildSharedProjectionAcceptanceRowsSortsByPrimaryKey pins the global
// lock order: rows come out sorted by (scope_id, acceptance_unit_id,
// source_run_id) regardless of map iteration order, so concurrent batches
// over overlapping keys cannot lock them in opposite orders (#6679).
func TestBuildSharedProjectionAcceptanceRowsSortsByPrimaryKey(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	var intents []reducer.SharedProjectionIntentRow
	for _, key := range [][3]string{
		{"scope-b", "unit-a", "run-1"},
		{"scope-a", "unit-b", "run-1"},
		{"scope-a", "unit-a", "run-2"},
		{"scope-a", "unit-a", "run-1"},
		{"scope-c", "unit-a", "run-1"},
		{"scope-a", "unit-c", "run-9"},
	} {
		intent := staleWriteIntent("gen-1", now)
		intent.IntentID = key[0] + "|" + key[1] + "|" + key[2]
		intent.ScopeID, intent.AcceptanceUnitID, intent.SourceRunID = key[0], key[1], key[2]
		intents = append(intents, intent)
	}

	want := []string{
		"scope-a|unit-a|run-1",
		"scope-a|unit-a|run-2",
		"scope-a|unit-b|run-1",
		"scope-a|unit-c|run-9",
		"scope-b|unit-a|run-1",
		"scope-c|unit-a|run-1",
	}
	// Map iteration is randomized per run; repeat to make an unsorted
	// implementation fail with overwhelming probability.
	for attempt := 0; attempt < 50; attempt++ {
		rows, err := buildSharedProjectionAcceptanceRows(intents)
		if err != nil {
			t.Fatalf("buildSharedProjectionAcceptanceRows() error = %v", err)
		}
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, row.ScopeID+"|"+row.AcceptanceUnitID+"|"+row.SourceRunID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("attempt %d: row order = %v, want %v", attempt, got, want)
		}
	}
}

// TestRecordSharedAcceptanceStaleWritesLabelsUnmappedKeyUnknown keeps the
// domain label closed: a stale row whose key maps to no intent is counted
// under "unknown", never under an empty label.
func TestRecordSharedAcceptanceStaleWritesLabelsUnmappedKeyUnknown(t *testing.T) {
	t.Parallel()

	instruments, reader := newStaleWriteInstruments(t)
	stale := []SharedProjectionAcceptance{{
		ScopeID: "scope-unmapped", AcceptanceUnitID: "unit-unmapped", SourceRunID: "run-unmapped", GenerationID: "gen-1",
	}}
	recordSharedAcceptanceStaleWrites(context.Background(), instruments, nil, stale)

	got := staleWritesByDomain(t, reader)
	if got[sharedAcceptanceStaleUnknownDomain] != 1 || len(got) != 1 {
		t.Fatalf("stale writes = %v, want {%s: 1}", got, sharedAcceptanceStaleUnknownDomain)
	}
}
