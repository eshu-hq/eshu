// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Source says where a status answer came from.
type Source string

const (
	// SourceModel is a stored row that passed every fence.
	SourceModel Source = "model"
	// SourceLive is the live statement run because the reader flag is off.
	SourceLive Source = "live"
	// SourceLiveFallback is the live statement run because the stored row was
	// missing, foreign, or too old. The answer carries the Reason.
	SourceLiveFallback Source = "live_fallback"
)

// Reason is the closed set of values that explain a Source. It is a metric
// label, so add a value only with its row in the telemetry reference.
type Reason string

const (
	// ReasonFresh: the stored row passed every fence and is served.
	ReasonFresh Reason = "fresh"
	// ReasonFlagOff: the reader flag is off, so the live statement ran.
	ReasonFlagOff Reason = "flag_off"
	// ReasonMissing: no row is stored for the model key yet.
	ReasonMissing Reason = "missing"
	// ReasonNotInstalled: migration 161 has not been applied to this database.
	ReasonNotInstalled Reason = "not_installed"
	// ReasonVersion: the row's schema version or statement digest differs from
	// this binary's, as in a rolling upgrade.
	ReasonVersion Reason = "version"
	// ReasonRowCount: the stored row_count disagrees with its payload.
	ReasonRowCount Reason = "row_count"
	// ReasonStale: the row is older than the configured stale_after.
	ReasonStale Reason = "stale"
	// ReasonDecode: the payload or an age key could not be decoded.
	ReasonDecode Reason = "decode"
)

// clockSQL reads the database clock and whether the model table exists, in
// one statement with no relation access, so a missing table is detected
// without the undefined_table error that would abort the status snapshot
// transaction. The age of a row is this clock minus its as_of: both come from
// the database, never from the caller's clock.
const clockSQL = `SELECT clock_timestamp(), to_regclass('status_summary_snapshots') IS NOT NULL`

// SelectConfig is the reader's fence settings for one model.
type SelectConfig struct {
	// ModelKey is the row to read.
	ModelKey string
	// SourceSHA256 is the digest of the statement text this binary would run
	// live. A row written by another statement is never decoded.
	SourceSHA256 string
	// StaleAfter is the oldest age a row may have and still be served.
	StaleAfter time.Duration
}

// Selection is the outcome of one Select.
type Selection struct {
	// Source is SourceModel when Entries are served and SourceLiveFallback
	// when the caller must run the live statement.
	Source Source
	// Reason explains Source.
	Reason Reason
	// AsOf is the stored row's as_of; zero when no row was read.
	AsOf time.Time
	// Age is the database clock minus AsOf, never negative; zero when no row
	// was read. For a stale fallback it is the age that was rejected.
	Age time.Duration
	// Entries is the stored result with its age keys advanced by Age. It is
	// set only when Source is SourceModel.
	Entries []Entry
}

// Select reads the stored row for cfg.ModelKey and decides whether it can be
// served. It never returns a stale, foreign, or undecodable row: each of
// those is a typed fallback and the caller runs the live statement instead,
// so one answer never mixes a stored row with a live read. A database error
// is returned, not turned into a fallback: inside a snapshot transaction a
// failed statement aborts the transaction, and a wrong answer must not hide
// behind a quiet fallback. Both statements must run on the status snapshot
// transaction so the row is the newest version that snapshot can see.
func Select(ctx context.Context, queryer db.Queryer, cfg SelectConfig) (Selection, error) {
	if strings.TrimSpace(cfg.ModelKey) == "" || strings.TrimSpace(cfg.SourceSHA256) == "" || cfg.StaleAfter <= 0 {
		return Selection{}, errors.New("select status summary: model key, source digest, and a positive stale_after are required")
	}
	now, installed, err := readClock(ctx, queryer)
	if err != nil {
		return Selection{}, err
	}
	if !installed {
		return fallback(ReasonNotInstalled, Row{}, 0), nil
	}
	row, err := Read(ctx, queryer, cfg.ModelKey)
	switch {
	case errors.Is(err, ErrNotFound):
		return fallback(ReasonMissing, Row{}, 0), nil
	case errors.Is(err, ErrRowCountMismatch):
		return fallback(ReasonRowCount, Row{}, 0), nil
	case errors.Is(err, ErrDecode):
		return fallback(ReasonDecode, Row{}, 0), nil
	case err != nil:
		return Selection{}, err
	}
	age := now.Sub(row.AsOf)
	if age < 0 {
		age = 0
	}
	if row.SchemaVersion != SchemaVersion || row.SourceSHA256 != cfg.SourceSHA256 {
		return fallback(ReasonVersion, row, age), nil
	}
	if age > cfg.StaleAfter {
		return fallback(ReasonStale, row, age), nil
	}
	entries, err := AddAge(row.Entries, age)
	if err != nil {
		return fallback(ReasonDecode, row, age), nil
	}
	return Selection{Source: SourceModel, Reason: ReasonFresh, AsOf: row.AsOf, Age: age, Entries: entries}, nil
}

func fallback(reason Reason, row Row, age time.Duration) Selection {
	return Selection{Source: SourceLiveFallback, Reason: reason, AsOf: row.AsOf, Age: age}
}

func readClock(ctx context.Context, queryer db.Queryer) (time.Time, bool, error) {
	rows, err := queryer.QueryContext(ctx, clockSQL)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read status summary clock: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, false, fmt.Errorf("read status summary clock: %w", err)
		}
		return time.Time{}, false, errors.New("read status summary clock: no result row")
	}
	var (
		now       time.Time
		installed bool
	)
	if err := rows.Scan(&now, &installed); err != nil {
		return time.Time{}, false, fmt.Errorf("scan status summary clock: %w", err)
	}
	return now.UTC(), installed, rows.Err()
}
