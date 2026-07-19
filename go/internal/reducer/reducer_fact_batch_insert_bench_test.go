// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// benchFactRows builds n deterministic reducerFactRow values for the insert
// benchmark. Distinct fact_ids so no dedupe collapse skews the row count.
func benchFactRows(n int) []reducerFactRow {
	now := time.Now().UTC()
	rows := make([]reducerFactRow, n)
	for i := range rows {
		rows[i] = reducerFactRow{
			FactID:           fmt.Sprintf("bench-fact:%08d", i),
			ScopeID:          "bench-scope",
			GenerationID:     "bench-gen-1",
			FactKind:         "bench_reducer_fact",
			StableFactKey:    fmt.Sprintf("bench-sk:%08d", i),
			CollectorKind:    "reducer",
			SourceConfidence: "reported",
			SourceSystem:     "bench",
			SourceFactKey:    fmt.Sprintf("bench-sfk:%08d", i),
			ObservedAt:       now,
			IngestedAt:       now,
			IsTombstone:      false,
			Payload:          `{"benchmark":true,"n":1}`,
		}
	}
	return rows
}

// insertReducerFactPerRow reproduces the pre-#5317 per-row loop: one ExecContext
// round-trip per fact row through canonicalReducerFactInsertQuery.
func insertReducerFactPerRow(ctx context.Context, db workloadIdentityExecer, rows []reducerFactRow) error {
	for _, r := range rows {
		if _, err := db.ExecContext(ctx, canonicalReducerFactInsertQuery,
			r.FactID, r.ScopeID, r.GenerationID, r.FactKind, r.StableFactKey,
			r.CollectorKind, r.SourceConfidence, r.SourceSystem, r.SourceFactKey,
			r.SourceURI, r.SourceRecordID, r.ObservedAt, r.IngestedAt, r.IsTombstone, r.Payload,
		); err != nil {
			return err
		}
	}
	return nil
}

// BenchmarkReducerFactInsertPerRowVsBatched measures the wall-clock cost of the
// pre-#5317 per-row loop against the batched unnest insert on a live Postgres.
// Gated on ESHU_POSTGRES_DSN (a DB with the fact_records schema applied):
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@127.0.0.1:15499/eshu \
//	  go test ./internal/reducer/ -run '^$' \
//	  -bench BenchmarkReducerFactInsertPerRowVsBatched -benchmem
func BenchmarkReducerFactInsertPerRowVsBatched(b *testing.B) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		b.Skip("set ESHU_POSTGRES_DSN (a Postgres with the fact_records schema) to run this benchmark")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		b.Fatalf("ping postgres: %v", err)
	}
	ctx := context.Background()

	// Seed the FK parents (fact_records.scope_id -> ingestion_scopes,
	// generation_id -> scope_generations) so the inserts satisfy their
	// constraints. Idempotent.
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		VALUES ('bench-scope','bench','bench','bench-key','reducer','bench-part',$1,$1,'active')
		ON CONFLICT (scope_id) DO NOTHING`, now); err != nil {
		b.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
		VALUES ('bench-gen-1','bench-scope','bench',false,$1,$1,'active')
		ON CONFLICT (generation_id) DO NOTHING`, now); err != nil {
		b.Fatalf("seed scope_generations: %v", err)
	}

	for _, n := range []int{100, 1000} {
		rows := benchFactRows(n)
		b.Run(fmt.Sprintf("per_row/N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := insertReducerFactPerRow(ctx, db, rows); err != nil {
					b.Fatalf("per-row insert: %v", err)
				}
			}
		})
		b.Run(fmt.Sprintf("batched/N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := reducerBatchInsertFacts(ctx, db, rows); err != nil {
					b.Fatalf("batched insert: %v", err)
				}
			}
		})
	}
}
