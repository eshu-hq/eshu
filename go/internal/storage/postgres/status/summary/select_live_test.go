// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// selectDigest is the statement digest the live Select tests treat as this
// binary's own.
var selectDigest = strings.Repeat("cd", 32)

func selectConfig() summary.SelectConfig {
	return summary.SelectConfig{ModelKey: summary.ModelActiveWorkSummary, SourceSHA256: selectDigest, StaleAfter: 33 * time.Second}
}

// databaseNow reads the server clock the way the reader does.
func databaseNow(ctx context.Context, t *testing.T, database *sql.DB) time.Time {
	t.Helper()
	var now time.Time
	if err := database.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("read the database clock: %v", err)
	}
	return now.UTC()
}

// writeSelectRow replaces the stored row with a one-queue-entry row whose as_of
// is age before the database clock. It deletes first because the upsert guard
// refuses an older as_of, and these cases step the as_of backwards.
func writeSelectRow(ctx context.Context, t *testing.T, database *sql.DB, age time.Duration, mutate func(*summary.Row)) summary.Row {
	t.Helper()
	if _, err := database.ExecContext(ctx, `DELETE FROM status_summary_snapshots`); err != nil {
		t.Fatalf("clear the stored row: %v", err)
	}
	row := summary.Row{
		ModelKey: summary.ModelActiveWorkSummary, SchemaVersion: summary.SchemaVersion, SourceSHA256: selectDigest,
		AsOf: databaseNow(ctx, t, database).Add(-age), PassDuration: 300 * time.Millisecond, RowCount: 1,
		Entries: []summary.Entry{{Section: "queue", Ordinal: 1, JSON: `{"outstanding_count":2,"oldest_outstanding_age_seconds":5}`}},
	}
	if mutate != nil {
		mutate(&row)
	}
	if _, err := summary.NewStore(poolStore{database}).Upsert(ctx, row); err != nil {
		t.Fatalf("store the row: %v", err)
	}
	return row
}

// selectInSnapshot runs Select on a REPEATABLE READ READ ONLY transaction, the
// way the status snapshot does, and proves the transaction is still usable
// afterwards: a fallback must never leave it aborted.
func selectInSnapshot(ctx context.Context, t *testing.T, database *sql.DB) summary.Selection {
	t.Helper()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	selection, err := summary.Select(ctx, txStore{tx}, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("the snapshot transaction is unusable after Select: %v", err)
	}
	return selection
}

// TestStatusSummarySelectLive runs the reader's decision against a real
// database inside a read-only snapshot transaction: a missing table is a
// typed fallback that leaves the transaction usable (no undefined_table abort),
// a missing row, a foreign digest, and a stale row fall back, and a fresh row
// is served with its age taken from the database clock.
func TestStatusSummarySelectLive(t *testing.T) {
	ctx, database := openProofDatabase(t)

	t.Run("table not installed", func(t *testing.T) {
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonNotInstalled {
			t.Fatalf("selection = %+v, want live_fallback/not_installed", got)
		}
	})

	applySummaryMigration(ctx, t, database)

	t.Run("row missing", func(t *testing.T) {
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonMissing {
			t.Fatalf("selection = %+v, want live_fallback/missing", got)
		}
	})
	t.Run("fresh row is served with the database clock age added", func(t *testing.T) {
		row := writeSelectRow(ctx, t, database, 10*time.Second, nil)
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceModel || got.Reason != summary.ReasonFresh {
			t.Fatalf("selection = %+v, want model/fresh", got)
		}
		if !got.AsOf.Equal(row.AsOf) {
			t.Fatalf("as_of = %v, want %v", got.AsOf, row.AsOf)
		}
		if got.Age < 10*time.Second || got.Age > 15*time.Second {
			t.Fatalf("age = %v, want the 10s the row was backdated against the database clock (plus test latency)", got.Age)
		}
		var fields map[string]float64
		if err := json.Unmarshal([]byte(got.Entries[0].JSON), &fields); err != nil {
			t.Fatal(err)
		}
		if want := 5 + got.Age.Seconds(); fields["oldest_outstanding_age_seconds"] < want-0.001 || fields["oldest_outstanding_age_seconds"] > want+0.001 {
			t.Fatalf("aged queue age = %v, want 5 + %v", fields["oldest_outstanding_age_seconds"], got.Age.Seconds())
		}
		if fields["outstanding_count"] != 2 {
			t.Fatalf("a count changed: %v", fields)
		}
	})
	t.Run("stale row falls back", func(t *testing.T) {
		writeSelectRow(ctx, t, database, 40*time.Second, nil)
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonStale || got.Age < 40*time.Second {
			t.Fatalf("selection = %+v, want live_fallback/stale with the rejected 40s age", got)
		}
	})
	t.Run("foreign digest falls back", func(t *testing.T) {
		writeSelectRow(ctx, t, database, 1*time.Second, func(r *summary.Row) { r.SourceSHA256 = strings.Repeat("ee", 32) })
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonVersion {
			t.Fatalf("selection = %+v, want live_fallback/version", got)
		}
	})
	t.Run("foreign schema version falls back", func(t *testing.T) {
		writeSelectRow(ctx, t, database, 0, func(r *summary.Row) { r.SchemaVersion = summary.SchemaVersion + 1 })
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonVersion {
			t.Fatalf("selection = %+v, want live_fallback/version", got)
		}
	})
	t.Run("a payload that is not tuples falls back as decode", func(t *testing.T) {
		if _, err := database.ExecContext(ctx,
			`UPDATE status_summary_snapshots SET schema_version = $1, source_sha256 = $2, as_of = clock_timestamp(), rows = '{"x":1}'::jsonb, row_count = 1`,
			summary.SchemaVersion, selectDigest); err != nil {
			t.Fatal(err)
		}
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonDecode {
			t.Fatalf("selection = %+v, want live_fallback/decode", got)
		}
	})
	t.Run("a stored row_count that disagrees falls back", func(t *testing.T) {
		if _, err := database.ExecContext(ctx,
			`UPDATE status_summary_snapshots SET rows = '[["queue",1,"{}"]]'::jsonb, row_count = 7`); err != nil {
			t.Fatal(err)
		}
		got := selectInSnapshot(ctx, t, database)
		if got.Source != summary.SourceLiveFallback || got.Reason != summary.ReasonRowCount {
			t.Fatalf("selection = %+v, want live_fallback/row_count", got)
		}
	})
}
