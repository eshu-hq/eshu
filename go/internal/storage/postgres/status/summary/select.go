// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"encoding/json"
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
	// DecodeStale makes a stale row that passes every other fence also decode,
	// so the caller can keep it as its last row. The result is still a stale
	// fallback with no Entries; only Stored is set. Only the runtime /metrics
	// scrape sets it. A status route leaves it off and pays nothing for it.
	DecodeStale bool
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
	// was read. For a rejected row it is the age that was rejected.
	Age time.Duration
	// SignedAge is the same difference before the clamp: negative when the
	// writer's clock is ahead of the reader's. It is for the span only, so a
	// reader clock running behind is visible.
	SignedAge time.Duration
	// Entries is the stored result with its age keys advanced by Age. It is
	// set only when Source is SourceModel.
	Entries []Entry
	// Stored is the same result before the age advance, as the writer stored
	// it at AsOf. It is set when Source is SourceModel, and, only under
	// SelectConfig.DecodeStale, on a stale fallback whose row decodes and can be
	// aged. The scrape path keeps it as its last row and advances it again at
	// each read.
	Stored []Entry
	// Now is the database clock the read used; zero only when the clock read
	// failed.
	Now time.Time
}

// Select reads the stored row for cfg.ModelKey and decides whether it can be
// served. It never returns a stale, foreign, or undecodable row: each of
// those is a typed fallback and the caller runs the live statement instead,
// so one answer never mixes a stored row with a live read. The fences run in
// this order, from the cheapest scalar check to the payload decode: missing,
// not installed, version (schema version, then statement digest), row count,
// stale, decode. A row from another statement version is therefore never
// decoded and never counted as corrupt. A database error is returned, not
// turned into a fallback: inside a snapshot transaction a failed statement
// aborts the transaction, and a wrong answer must not hide behind a quiet
// fallback.
//
// Both statements must run on one transaction for the row to be the newest
// version that snapshot can see. The API and MCP server run them on the status
// snapshot transaction (REPEATABLE READ READ ONLY). Hosted runtimes build the
// status store once at startup and run each statement in autocommit, so the
// clock and the row come from separate statements there; a writer commit
// between them can only make the age negative, which clamps to zero.
func Select(ctx context.Context, queryer db.Queryer, cfg SelectConfig) (Selection, error) {
	if strings.TrimSpace(cfg.ModelKey) == "" || strings.TrimSpace(cfg.SourceSHA256) == "" || cfg.StaleAfter <= 0 {
		return Selection{}, errors.New("select status summary: model key, source digest, and a positive stale_after are required")
	}
	now, installed, err := readClock(ctx, queryer)
	if err != nil {
		return Selection{}, err
	}
	if !installed {
		return fallback(now, ReasonNotInstalled, Row{}, 0, 0), nil
	}
	row, payload, err := readRaw(ctx, queryer, cfg.ModelKey)
	switch {
	case errors.Is(err, ErrNotFound):
		return fallback(now, ReasonMissing, Row{}, 0, 0), nil
	case err != nil:
		return Selection{}, err
	}
	signedAge := now.Sub(row.AsOf)
	age := max(signedAge, 0)
	if row.SchemaVersion != SchemaVersion || row.SourceSHA256 != cfg.SourceSHA256 {
		return fallback(now, ReasonVersion, row, age, signedAge), nil
	}
	if count, countable := payloadLength(payload); countable && count != row.RowCount {
		return fallback(now, ReasonRowCount, row, age, signedAge), nil
	}
	if age > cfg.StaleAfter {
		stale := fallback(now, ReasonStale, row, age, signedAge)
		if cfg.DecodeStale {
			stale.Stored = decodableStored(payload, row.RowCount, age)
		}
		return stale, nil
	}
	if row.Entries, err = DecodeEntries(payload); err != nil {
		return fallback(now, ReasonDecode, row, age, signedAge), nil
	}
	if row.RowCount != len(row.Entries) {
		return fallback(now, ReasonRowCount, row, age, signedAge), nil
	}
	entries, err := AddAge(row.Entries, age)
	if err != nil {
		return fallback(now, ReasonDecode, row, age, signedAge), nil
	}
	return Selection{Source: SourceModel, Reason: ReasonFresh, AsOf: row.AsOf, Age: age, Entries: entries, Stored: row.Entries, SignedAge: signedAge, Now: now}, nil
}

// decodableStored returns the entries of a row that passed the version and row
// count fences when they decode, match the stored row_count, and their age keys
// can be advanced; otherwise nil. It is the part of the fence order after
// stale, run only for a caller that serves a stale row.
func decodableStored(payload []byte, rowCount int, age time.Duration) []Entry {
	entries, err := DecodeEntries(payload)
	if err != nil || rowCount != len(entries) {
		return nil
	}
	// Probe with at least a second: AddAge never fails on a non-positive age,
	// so the floor forces real validation of the stored entries.
	if _, err := AddAge(entries, max(age, time.Second)); err != nil {
		return nil
	}
	return entries
}

// readRaw reads the row with the same keyed statement as Read but returns the
// payload undecoded, so Select can judge the scalar columns first. A missing
// table, a missing row, and other failures are classified like Read's.
func readRaw(ctx context.Context, queryer db.Queryer, modelKey string) (Row, []byte, error) {
	rows, err := queryer.QueryContext(ctx, readSQL, modelKey)
	if err != nil {
		return Row{}, nil, classify(fmt.Errorf("read status summary %q: %w", modelKey, err))
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Row{}, nil, classify(fmt.Errorf("read status summary %q: %w", modelKey, err))
		}
		return Row{}, nil, fmt.Errorf("%w: model %q", ErrNotFound, modelKey)
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
		return Row{}, nil, classify(fmt.Errorf("scan status summary %q: %w", modelKey, err))
	}
	row.PassDuration = time.Duration(durationMs * float64(time.Millisecond))
	return row, payload, nil
}

// payloadLength counts the top-level elements of a stored payload without
// decoding them. It reports false when the payload is not a JSON array, which
// is a decode problem, not a row count problem.
func payloadLength(payload []byte) (int, bool) {
	var elements []json.RawMessage
	if err := json.Unmarshal(payload, &elements); err != nil || elements == nil {
		return 0, false
	}
	return len(elements), true
}

func fallback(now time.Time, reason Reason, row Row, age, signedAge time.Duration) Selection {
	return Selection{Source: SourceLiveFallback, Reason: reason, AsOf: row.AsOf, Age: age, SignedAge: signedAge, Now: now}
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
