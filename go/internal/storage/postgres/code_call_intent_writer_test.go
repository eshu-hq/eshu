// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestCodeCallIntentWriterRecordsAcceptanceUpsertMetrics(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("postgres-code-call-writer"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	database := &codeCallIntentWriterTestDB{}
	writer := NewCodeCallIntentWriterWithInstruments(database, instruments)
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-1",
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "caller->callee",
			ScopeID:          "scope:git:repo-1",
			AcceptanceUnitID: "repository:repo-1",
			RepositoryID:     "repository:repo-1",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload: map[string]any{
				"caller_entity_id": "entity:caller",
				"callee_entity_id": "entity:callee",
			},
			CreatedAt: now,
		},
	}

	if err := writer.UpsertIntents(context.Background(), rows); err != nil {
		t.Fatalf("UpsertIntents() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := codeCallWriterCounterValue(t, rm, "eshu_dp_shared_acceptance_upserts_total"); got != 1 {
		t.Fatalf("eshu_dp_shared_acceptance_upserts_total = %d, want 1", got)
	}
	if got := codeCallWriterHistogramCount(t, rm, "eshu_dp_shared_acceptance_upsert_duration_seconds"); got != 1 {
		t.Fatalf("eshu_dp_shared_acceptance_upsert_duration_seconds count = %d, want 1", got)
	}
}

type codeCallIntentWriterTestDB struct {
	intentWrites      int
	acceptanceWrites  int
	storedIntentIDs   []string
	storedAcceptances []string
	staleKeys         map[string]struct{}
}

func (database *codeCallIntentWriterTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		database.intentWrites++
		for i := 0; i < len(args); i += columnsPerSharedIntent {
			database.storedIntentIDs = append(database.storedIntentIDs, args[i].(string))
		}
		return sharedIntentResult{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

// QueryContext serves the acceptance upsert, which reports applied keys via
// RETURNING (#6679). Keys listed in staleKeys are withheld from RETURNING to
// simulate the advance-only guard skipping them.
func (database *codeCallIntentWriterTestDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if !strings.Contains(query, "INSERT INTO shared_projection_acceptance") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	database.acceptanceWrites++
	returned := &fake.Rows{}
	for i := 0; i < len(args); i += acceptanceColumnsPerRow {
		key := fmt.Sprintf("%s|%s|%s", args[i].(string), args[i+1].(string), args[i+2].(string))
		if _, stale := database.staleKeys[key]; stale {
			continue
		}
		database.storedAcceptances = append(database.storedAcceptances, key)
		returned.Data = append(returned.Data, []any{args[i], args[i+1], args[i+2]})
	}
	return returned, nil
}

func codeCallWriterCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, m.Data)
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}

	t.Fatalf("metric %s not found", metricName)
	return 0
}

func codeCallWriterHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string) uint64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, m.Data)
			}
			var total uint64
			for _, dp := range histogram.DataPoints {
				total += dp.Count
			}
			return total
		}
	}

	t.Fatalf("metric %s not found", metricName)
	return 0
}
