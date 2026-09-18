// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestChangedSinceDuplicatePayloadSetsLive proves counts and samples classify
// every payload in a stable-key group on a real PostgreSQL backend.
func TestChangedSinceDuplicatePayloadSetsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the changed-since payload-set proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin isolated fixture: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE fact_records (
    scope_id text NOT NULL,
    generation_id text NOT NULL,
    fact_kind text NOT NULL,
    stable_fact_key text NOT NULL,
    is_tombstone boolean NOT NULL,
    payload jsonb NOT NULL
) ON COMMIT DROP;
SET LOCAL search_path = pg_temp;
`); err != nil {
		t.Fatalf("create isolated fact fixture: %v", err)
	}
	rows := []struct {
		generation string
		key        string
		payload    string
		tombstone  bool
	}{
		{"prior", "dup_changed", `{"v":"G"}`, false},
		{"prior", "dup_changed", `{"v":"A"}`, false},
		{"current", "dup_changed", `{"v":"G"}`, false},
		{"current", "dup_changed", `{"v":"B"}`, false},
		{"prior", "reordered", `{"v":"G"}`, false},
		{"prior", "reordered", `{"v":"A"}`, false},
		{"current", "reordered", `{"v":"A"}`, false},
		{"current", "reordered", `{"v":"G"}`, false},
		{"prior", "multiplicity", `{"v":"A"}`, false},
		{"prior", "multiplicity", `{"v":"A"}`, false},
		{"current", "multiplicity", `{"v":"A"}`, false},
		{"prior", "singleton", `{"v":"A"}`, false},
		{"current", "singleton", `{"v":"B"}`, false},
		{"prior", "retired", `{"v":"A"}`, false},
		{"current", "retired", `{"v":"A"}`, true},
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (scope_id, generation_id, fact_kind, stable_fact_key, is_tombstone, payload)
VALUES ('probe', $1, 'test', $2, $3, $4::jsonb)`, row.generation, row.key, row.tombstone, row.payload); err != nil {
			t.Fatalf("insert %s/%s: %v", row.generation, row.key, err)
		}
	}
	counts, err := tx.QueryContext(ctx, changedSinceCountsQuery, "probe", "prior", "current")
	if err != nil {
		t.Fatalf("changed-since counts: %v", err)
	}
	gotCounts := map[string]int64{}
	for counts.Next() {
		var category, classification string
		var count int64
		if err := counts.Scan(&category, &classification, &count); err != nil {
			t.Fatalf("scan count: %v", err)
		}
		if category != "facts" {
			t.Fatalf("unexpected category %q", category)
		}
		gotCounts[classification] = count
	}
	if err := counts.Err(); err != nil {
		t.Fatalf("read counts: %v", err)
	}
	_ = counts.Close()
	for classification, want := range map[string]int64{"updated": 3, "unchanged": 1, "retired": 1} {
		if gotCounts[classification] != want {
			t.Errorf("%s count = %d, want %d (all counts: %v)", classification, gotCounts[classification], want, gotCounts)
		}
	}
	if len(gotCounts) != 3 {
		t.Errorf("unexpected classification counts: %v", gotCounts)
	}

	samples, err := tx.QueryContext(ctx, changedSinceSamplesQuery, "probe", "prior", "current", "facts", "updated", 10)
	if err != nil {
		t.Fatalf("changed-since samples: %v", err)
	}
	var gotKeys []string
	for samples.Next() {
		var key, kind string
		if err := samples.Scan(&key, &kind); err != nil {
			t.Fatalf("scan sample: %v", err)
		}
		if kind != "test" {
			t.Errorf("sample %q has fact_kind %q, want test", key, kind)
		}
		gotKeys = append(gotKeys, key)
	}
	if err := samples.Err(); err != nil {
		t.Fatalf("read samples: %v", err)
	}
	_ = samples.Close()
	if want := []string{"dup_changed", "multiplicity", "singleton"}; !slices.Equal(gotKeys, want) {
		t.Errorf("updated samples = %v, want %v", gotKeys, want)
	}
}
