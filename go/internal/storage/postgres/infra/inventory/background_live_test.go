// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// flakyBeginDB fails its first failures transactions, then behaves normally,
// so a backfill attempt fails and a later one succeeds.
type flakyBeginDB struct {
	postgres.SQLDB
	failures atomic.Int64
}

func (f *flakyBeginDB) Begin(ctx context.Context) (db.Transaction, error) {
	if f.failures.Add(-1) >= 0 {
		return nil, errors.New("injected transient begin failure")
	}
	return f.SQLDB.Begin(ctx)
}

// TestRunBackgroundLiveRetriesUntilTheMarkerIsRecorded: a failed backfill used
// to log once and exit, leaving readers on the graph until the process
// restarted. It must retry with backoff until the marker
// exists, and count every attempt by outcome.
func TestRunBackgroundLiveRetriesUntilTheMarkerIsRecorded(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	resetMarker(t, ctx, sqlDB)
	putContent(t, ctx, sqlDB, uniqueRepo(t), contentRow{"r1", "a.tf", "TerraformResource", "r.1", `{}`})

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("infra-inventory-background-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	database := &flakyBeginDB{SQLDB: postgres.SQLDB{DB: sqlDB}}
	database.failures.Store(2)
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := inventory.RunBackground(runCtx, database, inventory.BackgroundOptions{
		Instruments:    instruments,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
	}); err != nil {
		t.Fatalf("RunBackground() error = %v", err)
	}
	complete, err := inventory.BackfillComplete(ctx, postgres.SQLDB{DB: sqlDB})
	if err != nil || !complete {
		t.Fatalf("BackfillComplete() = %v, %v; want true after retries", complete, err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_infra_inventory_backfill_runs_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := point.Attributes.Value("outcome")
				got[outcome.AsString()] += point.Value
			}
		}
	}
	if got["failed"] != 2 || got["completed"] != 1 {
		t.Fatalf("backfill runs = %v, want failed=2 completed=1", got)
	}
	resetMarker(t, ctx, sqlDB)
}
