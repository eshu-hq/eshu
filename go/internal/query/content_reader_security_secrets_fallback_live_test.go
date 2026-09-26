// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// openDeferredSecretPool opens a second pool on the proof database whose
// connections carry secretlines.DeferredSessionSQL, the way bootstrap-index
// opens its pool.
func openDeferredSecretPool(t *testing.T, ctx context.Context, db *sql.DB) *sql.DB {
	t.Helper()
	config, err := pgx.ParseConfig(os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&config.Database); err != nil {
		t.Fatal(err)
	}
	pool := stdlib.OpenDB(*config, inventory.WriterConnectOption(secretlines.DeferredSessionSQL))
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// TestHardcodedSecretReadFallsBackUntilReadyLive proves the investigation read
// never answers from an incomplete side table. After a bulk load began and its
// content was written from a deferred session (side table empty), every one of
// the 28 argument sets returns the legacy scan's rows and reports
// legacy_scan; after the finalizer publishes ready, the same sets return
// identical rows from the side table and report side_table; a new bulk load
// takes the side table away again at once.
func TestHardcodedSecretReadFallsBackUntilReadyLive(t *testing.T) {
	ctx, db := openSecretProofDatabase(t)
	reader := NewContentReader(db)
	sqlDB := storagepostgres.SQLDB{DB: db}

	manual := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(manual)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	reader.WithInstruments(instruments)

	epoch, err := secretlines.BeginDeferral(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	deferred := openDeferredSecretPool(t, ctx, db)
	writer := secretProofWriter{
		t: t, ctx: ctx, writer: storagepostgres.NewContentWriter(storagepostgres.SQLDB{DB: deferred}),
	}
	writer.write(append(secretProofHandFiles(), secretProofBulkFiles(240)...), false)

	var side int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines`).Scan(&side); err != nil || side != 0 {
		t.Fatalf("setup: deferred load left %d side rows (%v), want 0", side, err)
	}

	rows, source, err := reader.InvestigateHardcodedSecretsWithSource(ctx, codequery.HardcodedSecretInvestigationRequest{Limit: 26})
	if err != nil || source != codequery.HardcodedSecretReadLegacyScan || len(rows) == 0 {
		t.Fatalf("read before ready = %d rows, source %q, err %v; want rows from the legacy scan", len(rows), source, err)
	}
	diffs, total, err := secretDifferentialDiffs(ctx, db, reader)
	if err != nil || len(diffs) != 0 || total == 0 {
		t.Fatalf("legacy-served reads diverge from the frozen legacy scan: total=%d diffs=%v err=%v", total, diffs, err)
	}

	result, err := secretlines.Finalize(ctx, sqlDB, epoch, secretlines.Options{Workers: 3, BatchSize: 50})
	if err != nil || !result.Published {
		t.Fatalf("Finalize = %+v, %v, want published", result, err)
	}
	assertSecretState(t, ctx, db, reader, "after the finalizer")
	if _, source, err = reader.InvestigateHardcodedSecretsWithSource(ctx, codequery.HardcodedSecretInvestigationRequest{Limit: 26}); err != nil ||
		source != codequery.HardcodedSecretReadSideTable {
		t.Fatalf("read after ready reported source %q, err %v, want side_table", source, err)
	}

	if _, err := secretlines.BeginDeferral(ctx, sqlDB); err != nil {
		t.Fatal(err)
	}
	if _, source, err = reader.InvestigateHardcodedSecretsWithSource(ctx, codequery.HardcodedSecretInvestigationRequest{Limit: 26}); err != nil ||
		source != codequery.HardcodedSecretReadLegacyScan {
		t.Fatalf("read after a new bulk load began reported source %q, err %v, want legacy_scan", source, err)
	}

	counts := hardcodedSecretReadCounts(t, manual)
	if counts["legacy_scan"] == 0 || counts["side_table"] == 0 {
		t.Fatalf("eshu_dp_hardcoded_secret_reads_total by source = %v, want both legacy_scan and side_table recorded", counts)
	}
}

func hardcodedSecretReadCounts(t *testing.T, reader metric.Reader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_hardcoded_secret_reads_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value("source")
				counts[value.AsString()] += point.Value
			}
		}
	}
	return counts
}
