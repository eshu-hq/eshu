// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// undefinedTableSQLState is Postgres' undefined_table error code (42P01).
const undefinedTableSQLState = "42P01"

var (
	// ErrNotInstalled reports that status_summary_snapshots does not exist
	// yet (SQLSTATE 42P01): migration 161 has not been applied to this
	// database. A writer skips and keeps looping; a reader falls back to the
	// live statement.
	ErrNotInstalled = errors.New("status summary snapshots table is not installed")
	// ErrNotFound reports that no row is stored for the requested model key,
	// for example before the first writer pass.
	ErrNotFound = errors.New("status summary row not found")
	// ErrRowCountMismatch reports a row whose stored row_count differs from
	// the number of entries in its payload.
	ErrRowCountMismatch = errors.New("status summary row_count does not match the payload")
	// ErrDecode reports a rows payload that is not a well formed
	// [[section, ordinal, section_json_text], ...] array.
	ErrDecode = errors.New("status summary payload cannot be decoded")
)

// Reader reads one stored model row.
type Reader interface {
	// Read returns the row stored for the model key.
	Read(ctx context.Context, modelKey string) (Row, error)
}

// Writer writes one stored model row.
type Writer interface {
	// Upsert stores the row and reports whether it advanced the stored as_of.
	Upsert(ctx context.Context, row Row) (advanced bool, err error)
}

// Store is the writer and reader surface of the summary table.
type Store interface {
	Reader
	Writer
}

// sqlStore binds the package functions to one connection or pool.
type sqlStore struct {
	database db.ExecQueryer
}

// NewStore returns a Store that runs every statement on database. A writer
// pass that must hold the advisory lock and write in one transaction calls
// TryLock and Upsert with its own transaction instead.
func NewStore(database db.ExecQueryer) Store {
	return sqlStore{database: database}
}

// Upsert stores row with one guarded single-row statement and reports whether
// the row advanced: true when it inserted the row or replaced an older as_of,
// false when the guard rejected it because the stored as_of is equal or newer.
// A guard rejection is not an error. The row is validated first, so an
// invalid row never reaches the database. A missing table returns an error
// that satisfies errors.Is(err, ErrNotInstalled); any other failure is
// returned wrapped with its SQLSTATE preserved.
func Upsert(ctx context.Context, exec db.Executor, row Row) (bool, error) {
	if err := row.Validate(); err != nil {
		return false, err
	}
	payload, err := EncodeEntries(row.Entries)
	if err != nil {
		return false, err
	}
	result, err := exec.ExecContext(ctx, upsertSQL,
		row.ModelKey, row.SchemaVersion, row.SourceSHA256, row.AsOf,
		float64(row.PassDuration)/float64(time.Millisecond), row.RowCount, string(payload),
	)
	if err != nil {
		return false, classify(fmt.Errorf("upsert status summary %q: %w", row.ModelKey, err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("upsert status summary %q: rows affected: %w", row.ModelKey, err)
	}
	return affected > 0, nil
}

// Read returns the row stored for modelKey with one primary-key lookup. A
// missing table returns ErrNotInstalled and a missing row returns ErrNotFound.
// A stored row whose payload cannot be decoded returns ErrDecode and one whose
// row_count disagrees with its entries returns ErrRowCountMismatch, so a
// caller never receives a partial or corrupt summary. It does not judge the
// row's schema version, source digest, or age: those are the caller's fences.
func Read(ctx context.Context, queryer db.Queryer, modelKey string) (Row, error) {
	if strings.TrimSpace(modelKey) == "" {
		return Row{}, errors.New("read status summary: model key is blank")
	}
	rows, err := queryer.QueryContext(ctx, readSQL, modelKey)
	if err != nil {
		return Row{}, classify(fmt.Errorf("read status summary %q: %w", modelKey, err))
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Row{}, classify(fmt.Errorf("read status summary %q: %w", modelKey, err))
		}
		return Row{}, fmt.Errorf("%w: model %q", ErrNotFound, modelKey)
	}
	var (
		row        Row
		durationMs float64
		payload    []byte
	)
	if err := rows.Scan(
		&row.ModelKey, &row.SchemaVersion, &row.SourceSHA256, &row.AsOf,
		&row.ComputedAt, &durationMs, &row.RowCount, &payload,
	); err != nil {
		return Row{}, classify(fmt.Errorf("scan status summary %q: %w", modelKey, err))
	}
	row.PassDuration = time.Duration(durationMs * float64(time.Millisecond))
	if row.Entries, err = DecodeEntries(payload); err != nil {
		return Row{}, fmt.Errorf("status summary %q: %w", modelKey, err)
	}
	if row.RowCount != len(row.Entries) {
		return Row{}, fmt.Errorf("%w: model %q stores row_count %d for %d entries",
			ErrRowCountMismatch, modelKey, row.RowCount, len(row.Entries))
	}
	return row, nil
}

// Upsert implements Writer.
func (s sqlStore) Upsert(ctx context.Context, row Row) (bool, error) {
	return Upsert(ctx, s.database, row)
}

// Read implements Reader.
func (s sqlStore) Read(ctx context.Context, modelKey string) (Row, error) {
	return Read(ctx, s.database, modelKey)
}

// classify maps an undefined_table failure to ErrNotInstalled and leaves any
// other error untouched. The original error stays in the chain so a caller
// can still read the SQLSTATE through errors.As.
func classify(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == undefinedTableSQLState {
		return fmt.Errorf("%w: %w", ErrNotInstalled, err)
	}
	return err
}
