// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openProofDatabase returns a disposable PostgreSQL database that is empty:
// the summary migration is NOT applied. The live tests need an administrative
// DSN for a disposable server; see the header of store_live_test.go.
func openProofDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	return postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_STATUS_SUMMARY_PROOF_DSN"),
		os.Getenv("ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
}

// summaryMigrationSQL returns the embedded migration that creates
// status_summary_snapshots, so a test applies the shipped file and not a copy.
func summaryMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, definition := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/161_status_summary_snapshots.sql") {
			return definition.SQL
		}
	}
	t.Fatal("migration 161_status_summary_snapshots.sql is not embedded")
	return ""
}

// applySummaryMigration applies the shipped migration to database.
func applySummaryMigration(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, summaryMigrationSQL(t)); err != nil {
		t.Fatalf("apply migration 161: %v", err)
	}
}

// poolStore adapts a *sql.DB to the storage db contracts.
type poolStore struct{ database *sql.DB }

func (p poolStore) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.database.ExecContext(ctx, query, args...)
}

func (p poolStore) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return p.database.QueryContext(ctx, query, args...)
}

// txStore adapts a *sql.Tx to the storage db contracts.
type txStore struct{ tx *sql.Tx }

func (s txStore) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.tx.ExecContext(ctx, query, args...)
}

func (s txStore) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return s.tx.QueryContext(ctx, query, args...)
}

var proofAsOf = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// proofRow builds a valid row for key at asOf. The tag is embedded in every
// entry so two rows with the same as_of but different payloads are
// distinguishable, and entries sets the payload size.
func proofRow(asOf time.Time, tag string, entries int) summary.Row {
	row := summary.Row{
		ModelKey:      summary.ModelActiveWorkSummary,
		SchemaVersion: summary.SchemaVersion,
		SourceSHA256:  strings.Repeat("ab", 32),
		AsOf:          asOf,
		PassDuration:  412 * time.Millisecond,
		RowCount:      entries,
	}
	for i := 0; i < entries; i++ {
		row.Entries = append(row.Entries, summary.Entry{
			Section: fmt.Sprintf("section_%d", i%5),
			Ordinal: int64(i / 5),
			JSON:    fmt.Sprintf(`{"tag":%q,"i":%d,"n":%d,"age":12.5}`, tag, i, i*7),
		})
	}
	return row
}
