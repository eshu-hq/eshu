// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
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

// TestSharedIntentAcceptanceWriterStaleWriteCounterLive drives the production
// writer against real Postgres: G_new applies, a late G_old is skipped and
// counted once, and a same-generation G_new retry applies without counting.
func TestSharedIntentAcceptanceWriterStaleWriteCounterLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	instruments, reader := newStaleWriteInstruments(t)
	writer := NewSharedIntentAcceptanceWriterWithInstruments(SQLDB{DB: fixture.db}, instruments)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Microsecond)

	steps := []struct {
		name       string
		generation string
		at         time.Time
		wantStale  int64
	}{
		{name: "G_new applies", generation: fixture.genNew, at: base, wantStale: 0},
		{name: "late G_old is skipped", generation: fixture.genOld, at: base.Add(time.Minute), wantStale: 1},
		{name: "same-generation retry applies", generation: fixture.genNew, at: base.Add(2 * time.Minute), wantStale: 1},
	}
	for _, step := range steps {
		intent := staleWriteIntent(step.generation, step.at)
		intent.ScopeID = fixture.scopeID
		if err := writer.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{intent}); err != nil {
			t.Fatalf("%s: UpsertIntents() error = %v", step.name, err)
		}
		if got := staleWritesByDomain(t, reader)[reducer.DomainCodeCalls]; got != step.wantStale {
			t.Fatalf("%s: %s{domain=%s} = %d, want %d", step.name, staleWritesMetric, reducer.DomainCodeCalls, got, step.wantStale)
		}
		if got := fixture.accepted(t, "run-6679"); got != fixture.genNew {
			t.Fatalf("%s: accepted generation = %q, want %q", step.name, got, fixture.genNew)
		}
	}
}
