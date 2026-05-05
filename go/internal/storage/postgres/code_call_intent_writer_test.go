package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

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

	db := &codeCallIntentWriterTestDB{}
	writer := NewCodeCallIntentWriterWithInstruments(db, instruments)
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
}

func (db *codeCallIntentWriterTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		db.intentWrites++
		for i := 0; i < len(args); i += columnsPerSharedIntent {
			db.storedIntentIDs = append(db.storedIntentIDs, args[i].(string))
		}
		return sharedIntentResult{}, nil

	case strings.Contains(query, "INSERT INTO shared_projection_acceptance"):
		db.acceptanceWrites++
		for i := 0; i < len(args); i += acceptanceColumnsPerRow {
			key := fmt.Sprintf("%s|%s|%s", args[i].(string), args[i+1].(string), args[i+2].(string))
			db.storedAcceptances = append(db.storedAcceptances, key)
		}
		return sharedIntentResult{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *codeCallIntentWriterTestDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, fmt.Errorf("unexpected query")
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
