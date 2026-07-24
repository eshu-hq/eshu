// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
)

func TestCICDRunWatermarkSchemaSQL(t *testing.T) {
	t.Parallel()

	schema := CICDRunWatermarkSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS cicd_run_watermarks",
		"scope_id TEXT NOT NULL",
		"repository TEXT NOT NULL",
		"generation_id TEXT NOT NULL",
		"fencing_token BIGINT NOT NULL",
		"last_run_id TEXT NOT NULL",
		"PRIMARY KEY (scope_id, repository)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("CICDRunWatermarkSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}

func TestCICDRunWatermarkStoreSaveThenLoadRoundTrips(t *testing.T) {
	t.Parallel()

	db := &cicdWatermarkTestDB{
		execResults: []sql.Result{cicdWatermarkRowsResult{rowsAffected: 1}},
		queryRows: [][]any{
			{"200", "generation-1", int64(5), time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)},
		},
	}
	store := NewCICDRunWatermarkStore(db)
	key := runwatermark.Key{ScopeID: "ci-cd:github-actions:example/repo", Repository: "example/repo"}

	if err := store.Save(context.Background(), runwatermark.Watermark{
		Key: key, LastRunID: "200", GenerationID: "generation-1", FencingToken: 5,
	}); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	got, ok, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("Load() ok = false, want true")
	}
	if got.LastRunID != "200" || got.FencingToken != 5 {
		t.Fatalf("Load() = %+v, want LastRunID=200 FencingToken=5", got)
	}
}

func TestCICDRunWatermarkStoreLoadMissReturnsNotFound(t *testing.T) {
	t.Parallel()

	db := &cicdWatermarkTestDB{}
	store := NewCICDRunWatermarkStore(db)
	_, ok, err := store.Load(context.Background(), runwatermark.Key{ScopeID: "scope-1", Repository: "octo/repo"})
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if ok {
		t.Fatalf("Load() ok = true, want false for a miss")
	}
}

// TestCICDRunWatermarkStoreSaveRejectsOlderFence proves the store-level
// fencing guard: a superseded claim (0 rows affected because the SQL WHERE
// guard blocked the update) must surface runwatermark.ErrStaleFence, the
// same contract awscloud/checkpoint's Postgres store proves.
func TestCICDRunWatermarkStoreSaveRejectsOlderFence(t *testing.T) {
	t.Parallel()

	db := &cicdWatermarkTestDB{execResults: []sql.Result{cicdWatermarkRowsResult{rowsAffected: 0}}}
	store := NewCICDRunWatermarkStore(db)
	err := store.Save(context.Background(), runwatermark.Watermark{
		Key:          runwatermark.Key{ScopeID: "scope-1", Repository: "octo/repo"},
		LastRunID:    "100",
		GenerationID: "generation-1",
		FencingToken: 2,
	})
	if !errors.Is(err, runwatermark.ErrStaleFence) {
		t.Fatalf("Save() error = %v, want ErrStaleFence", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"ON CONFLICT (scope_id, repository) DO UPDATE",
		"WHERE cicd_run_watermarks.fencing_token <= EXCLUDED.fencing_token",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Save() query missing %q:\n%s", want, query)
		}
	}
}

func TestCICDRunWatermarkStoreSaveRejectsInvalidWatermark(t *testing.T) {
	t.Parallel()

	store := NewCICDRunWatermarkStore(&cicdWatermarkTestDB{})
	if err := store.Save(context.Background(), runwatermark.Watermark{}); err == nil {
		t.Fatalf("Save() error = nil, want validation error")
	}
}

func TestCICDRunWatermarkStoreLoadRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	store := NewCICDRunWatermarkStore(&cicdWatermarkTestDB{})
	if _, _, err := store.Load(context.Background(), runwatermark.Key{}); err == nil {
		t.Fatalf("Load() error = nil, want validation error")
	}
}

func TestCICDRunWatermarkStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	store := CICDRunWatermarkStore{}
	key := runwatermark.Key{ScopeID: "scope-1", Repository: "octo/repo"}
	if _, _, err := store.Load(context.Background(), key); err == nil {
		t.Fatalf("Load() error = nil, want database-required error")
	}
	err := store.Save(context.Background(), runwatermark.Watermark{
		Key: key, LastRunID: "1", GenerationID: "g", FencingToken: 1,
	})
	if err == nil {
		t.Fatalf("Save() error = nil, want database-required error")
	}
}

var _ runwatermark.Store = CICDRunWatermarkStore{}

type cicdWatermarkTestDB struct {
	execs       []cicdWatermarkExec
	execResults []sql.Result
	queryRows   [][]any
}

func (db *cicdWatermarkTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, cicdWatermarkExec{query: query, args: args})
	if len(db.execResults) == 0 {
		return cicdWatermarkRowsResult{rowsAffected: 1}, nil
	}
	result := db.execResults[0]
	db.execResults = db.execResults[1:]
	return result, nil
}

func (db *cicdWatermarkTestDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return &cicdWatermarkFakeRows{rows: db.queryRows}, nil
}

type cicdWatermarkExec struct {
	query string
	args  []any
}

type cicdWatermarkRowsResult struct{ rowsAffected int64 }

func (r cicdWatermarkRowsResult) LastInsertId() (int64, error) { return 0, nil }
func (r cicdWatermarkRowsResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

// cicdWatermarkFakeRows implements the postgres.Rows seam over a canned row
// set for tests, mirroring the AWS checkpoint store test's fake DB shape.
type cicdWatermarkFakeRows struct {
	rows [][]any
	idx  int
}

func (r *cicdWatermarkFakeRows) Next() bool {
	return r.idx < len(r.rows)
}

func (r *cicdWatermarkFakeRows) Scan(dest ...any) error {
	row := r.rows[r.idx]
	if len(row) != len(dest) {
		return errors.New("cicdWatermarkFakeRows: column count mismatch")
	}
	for i, value := range row {
		switch typed := dest[i].(type) {
		case *string:
			*typed, _ = value.(string)
		case *int64:
			*typed, _ = value.(int64)
		case *time.Time:
			*typed, _ = value.(time.Time)
		default:
			return errors.New("cicdWatermarkFakeRows: unsupported dest type")
		}
	}
	r.idx++
	return nil
}

func (r *cicdWatermarkFakeRows) Err() error { return nil }

func (r *cicdWatermarkFakeRows) Close() error { return nil }
