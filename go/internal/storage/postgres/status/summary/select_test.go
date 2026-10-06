// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const testSourceSHA = "feedface"

var testNow = time.Date(2026, 10, 6, 12, 0, 30, 0, time.UTC)

// scriptQueryer answers the selector's two statements from scripted rows and
// records every statement so a test can assert which ones ran.
type scriptQueryer struct {
	clock    *fakeRows
	clockErr error
	row      *fakeRows
	rowErr   error
	calls    []string
}

func (q *scriptQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.calls = append(q.calls, query)
	switch query {
	case clockSQL:
		return q.clock, q.clockErr
	case readSQL:
		return q.row, q.rowErr
	default:
		return nil, fmt.Errorf("unexpected statement %q", query)
	}
}

func (q *scriptQueryer) ranRead() bool {
	for _, call := range q.calls {
		if call == readSQL {
			return true
		}
	}
	return false
}

func clockRows(installed bool) *fakeRows {
	return &fakeRows{rows: [][]any{{testNow, installed}}}
}

// rowRows renders one stored row the way Read scans it.
func rowRows(row Row, payload string) *fakeRows {
	return &fakeRows{rows: [][]any{{
		row.ModelKey, row.SchemaVersion, row.SourceSHA256, row.AsOf, row.ComputedAt,
		float64(row.PassDuration) / float64(time.Millisecond), row.RowCount, []byte(payload),
	}}}
}

func selectConfig() SelectConfig {
	return SelectConfig{ModelKey: ModelActiveWorkSummary, SourceSHA256: testSourceSHA, StaleAfter: 33 * time.Second}
}

// freshStoredRow is a row written 10 s before testNow with one aged queue entry.
func freshStoredRow() Row {
	return Row{
		ModelKey:      ModelActiveWorkSummary,
		SchemaVersion: SchemaVersion,
		SourceSHA256:  testSourceSHA,
		AsOf:          testNow.Add(-10 * time.Second),
		RowCount:      1,
		Entries:       []Entry{{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":5}`}},
	}
}

func payloadOf(t *testing.T, row Row) string {
	t.Helper()
	encoded, err := EncodeEntries(row.Entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestSelectServesAFreshRowWithTheAgeAdded(t *testing.T) {
	t.Parallel()

	row := freshStoredRow()
	q := &scriptQueryer{clock: clockRows(true), row: rowRows(row, payloadOf(t, row))}
	got, err := Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceModel || got.Reason != ReasonFresh {
		t.Fatalf("source/reason = %s/%s, want model/fresh", got.Source, got.Reason)
	}
	if !got.AsOf.Equal(row.AsOf) || got.Age != 10*time.Second {
		t.Fatalf("as_of/age = %v/%v, want %v/10s", got.AsOf, got.Age, row.AsOf)
	}
	if len(got.Entries) != 1 || !strings.Contains(got.Entries[0].JSON, "15") {
		t.Fatalf("entries = %#v, want the 5 s queue age advanced by the 10 s row age to 15", got.Entries)
	}
}

func TestSelectAgeIsTheDatabaseClockNotTheCallerClock(t *testing.T) {
	t.Parallel()

	// The clock row the database returns is the only time source: a caller
	// clock cannot be passed in, and the clock statement runs in the same
	// transaction as the row read.
	row := freshStoredRow()
	q := &scriptQueryer{clock: clockRows(true), row: rowRows(row, payloadOf(t, row))}
	if _, err := Select(context.Background(), q, selectConfig()); err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(q.calls) != 2 || q.calls[0] != clockSQL || q.calls[1] != readSQL {
		t.Fatalf("statements = %q, want the clock then the keyed read", q.calls)
	}
	if !strings.Contains(clockSQL, "clock_timestamp()") {
		t.Fatalf("clockSQL = %q, want the database clock", clockSQL)
	}
}

func TestSelectFallbackMatrix(t *testing.T) {
	t.Parallel()

	stale := freshStoredRow()
	stale.AsOf = testNow.Add(-34 * time.Second)
	otherVersion := freshStoredRow()
	otherVersion.SchemaVersion = SchemaVersion + 1
	otherSHA := freshStoredRow()
	otherSHA.SourceSHA256 = "0ther"
	badCount := freshStoredRow()
	badCount.RowCount = 5
	otherSHAWrongCount := freshStoredRow()
	otherSHAWrongCount.SourceSHA256 = "0ther"
	otherSHAWrongCount.RowCount = 5
	staleWrongCount := freshStoredRow()
	staleWrongCount.AsOf = testNow.Add(-34 * time.Second)
	staleWrongCount.RowCount = 5

	for _, tc := range []struct {
		name       string
		q          *scriptQueryer
		want       Reason
		wantRead   bool
		wantAsOf   time.Time
		wantAgeSec int64
	}{
		{name: "table missing", q: &scriptQueryer{clock: clockRows(false)}, want: ReasonNotInstalled},
		{name: "row missing", q: &scriptQueryer{clock: clockRows(true), row: &fakeRows{}}, want: ReasonMissing, wantRead: true},
		{name: "schema version", q: &scriptQueryer{clock: clockRows(true), row: rowRows(otherVersion, payloadOf(t, otherVersion))}, want: ReasonVersion, wantRead: true, wantAsOf: otherVersion.AsOf, wantAgeSec: 10},
		{name: "source digest", q: &scriptQueryer{clock: clockRows(true), row: rowRows(otherSHA, payloadOf(t, otherSHA))}, want: ReasonVersion, wantRead: true, wantAsOf: otherSHA.AsOf, wantAgeSec: 10},
		{name: "row count", q: &scriptQueryer{clock: clockRows(true), row: rowRows(badCount, payloadOf(t, badCount))}, want: ReasonRowCount, wantRead: true, wantAsOf: badCount.AsOf, wantAgeSec: 10},
		{name: "foreign version beats an undecodable payload", q: &scriptQueryer{clock: clockRows(true), row: rowRows(otherVersion, `{"future":"encoding"}`)}, want: ReasonVersion, wantRead: true, wantAsOf: otherVersion.AsOf, wantAgeSec: 10},
		{name: "foreign digest beats a row count mismatch", q: &scriptQueryer{clock: clockRows(true), row: rowRows(otherSHAWrongCount, payloadOf(t, otherSHAWrongCount))}, want: ReasonVersion, wantRead: true, wantAsOf: otherSHAWrongCount.AsOf, wantAgeSec: 10},
		{name: "row count beats stale", q: &scriptQueryer{clock: clockRows(true), row: rowRows(staleWrongCount, payloadOf(t, staleWrongCount))}, want: ReasonRowCount, wantRead: true, wantAsOf: staleWrongCount.AsOf, wantAgeSec: 34},
		{name: "stale beats an undecodable payload", q: &scriptQueryer{clock: clockRows(true), row: rowRows(stale, `{"not":"tuples"}`)}, want: ReasonStale, wantRead: true, wantAsOf: stale.AsOf, wantAgeSec: 34},
		{name: "stale", q: &scriptQueryer{clock: clockRows(true), row: rowRows(stale, payloadOf(t, stale))}, want: ReasonStale, wantRead: true, wantAsOf: stale.AsOf, wantAgeSec: 34},
		{name: "payload decode", q: &scriptQueryer{clock: clockRows(true), row: rowRows(freshStoredRow(), `{"not":"tuples"}`)}, want: ReasonDecode, wantRead: true, wantAsOf: freshStoredRow().AsOf, wantAgeSec: 10},
		{name: "age key not a number", q: &scriptQueryer{clock: clockRows(true), row: rowRows(freshStoredRow(), `[["queue",1,"{\"oldest_outstanding_age_seconds\":\"x\"}"]]`)}, want: ReasonDecode, wantRead: true, wantAsOf: freshStoredRow().AsOf, wantAgeSec: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Select(context.Background(), tc.q, selectConfig())
			if err != nil {
				t.Fatalf("Select() error = %v, want a typed fallback", err)
			}
			if got.Source != SourceLiveFallback || got.Reason != tc.want {
				t.Fatalf("source/reason = %s/%s, want live_fallback/%s", got.Source, got.Reason, tc.want)
			}
			if len(got.Entries) != 0 {
				t.Fatalf("a fallback returned %d model entries; a stored row is never mixed with the live read", len(got.Entries))
			}
			if tc.q.ranRead() != tc.wantRead {
				t.Fatalf("row read issued = %v, want %v", tc.q.ranRead(), tc.wantRead)
			}
			if !got.AsOf.Equal(tc.wantAsOf) || got.Age != time.Duration(tc.wantAgeSec)*time.Second {
				t.Fatalf("as_of/age = %v/%v, want %v/%ds", got.AsOf, got.Age, tc.wantAsOf, tc.wantAgeSec)
			}
		})
	}
}

func TestSelectStaleBoundaryIsStrict(t *testing.T) {
	t.Parallel()

	atLimit := freshStoredRow()
	atLimit.AsOf = testNow.Add(-33 * time.Second)
	q := &scriptQueryer{clock: clockRows(true), row: rowRows(atLimit, payloadOf(t, atLimit))}
	got, err := Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceModel {
		t.Fatalf("a row exactly stale_after old: source = %s, want model (older than stale_after falls back)", got.Source)
	}

	justOver := freshStoredRow()
	justOver.AsOf = testNow.Add(-33*time.Second - time.Microsecond)
	q = &scriptQueryer{clock: clockRows(true), row: rowRows(justOver, payloadOf(t, justOver))}
	got, err = Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceLiveFallback || got.Reason != ReasonStale {
		t.Fatalf("a row just over stale_after: %s/%s, want live_fallback/stale", got.Source, got.Reason)
	}
}

func TestSelectClampsAFutureAsOfToZeroAge(t *testing.T) {
	t.Parallel()

	// A reader clock slightly behind the writer's makes as_of land in the
	// future; the age is zero, never negative, and the row is served.
	future := freshStoredRow()
	future.AsOf = testNow.Add(2 * time.Second)
	q := &scriptQueryer{clock: clockRows(true), row: rowRows(future, payloadOf(t, future))}
	got, err := Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceModel || got.Age != 0 {
		t.Fatalf("source/age = %s/%v, want model/0", got.Source, got.Age)
	}
}

func TestSelectFailsClosedOnDatabaseErrors(t *testing.T) {
	t.Parallel()

	boom := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	row := freshStoredRow()
	for _, tc := range []struct {
		name string
		q    *scriptQueryer
	}{
		{"clock read fails", &scriptQueryer{clockErr: boom}},
		{"keyed read fails", &scriptQueryer{clock: clockRows(true), rowErr: boom}},
		{"clock row missing", &scriptQueryer{clock: &fakeRows{}}},
		{"table vanished after the check", &scriptQueryer{clock: clockRows(true), rowErr: &pgconn.PgError{Code: undefinedTableSQLState}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_ = row
			got, err := Select(context.Background(), tc.q, selectConfig())
			if err == nil {
				t.Fatalf("Select() = %#v, error = nil; a database error must fail closed, not fall back", got)
			}
		})
	}
	var pgErr *pgconn.PgError
	_, err := Select(context.Background(), &scriptQueryer{clockErr: boom}, selectConfig())
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("error %v does not keep the SQLSTATE reachable", err)
	}
}

func TestSelectRejectsABlankConfig(t *testing.T) {
	t.Parallel()

	for _, cfg := range []SelectConfig{
		{SourceSHA256: testSourceSHA, StaleAfter: time.Second},
		{ModelKey: ModelActiveWorkSummary, StaleAfter: time.Second},
		{ModelKey: ModelActiveWorkSummary, SourceSHA256: testSourceSHA},
	} {
		if _, err := Select(context.Background(), &scriptQueryer{}, cfg); err == nil {
			t.Fatalf("Select(%+v) error = nil, want a config error", cfg)
		}
	}
}

// TestSelectRollingUpgradeFlipsBackToTheModel is design ruling D3.2 item 11,
// second half: a row written by another statement version is a version
// fallback and is never decoded, and the next read after the writer has
// replaced it with this binary's digest is served from the model.
func TestSelectRollingUpgradeFlipsBackToTheModel(t *testing.T) {
	t.Parallel()

	foreign := freshStoredRow()
	foreign.SourceSHA256 = "old-binary"
	q := &scriptQueryer{clock: clockRows(true), row: rowRows(foreign, `{"foreign":"payload nobody decodes"}`)}
	got, err := Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceLiveFallback || got.Reason != ReasonVersion {
		t.Fatalf("first read = %s/%s, want live_fallback/version", got.Source, got.Reason)
	}

	current := freshStoredRow()
	q = &scriptQueryer{clock: clockRows(true), row: rowRows(current, payloadOf(t, current))}
	got, err = Select(context.Background(), q, selectConfig())
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Source != SourceModel || got.Reason != ReasonFresh {
		t.Fatalf("second read = %s/%s, want model/fresh once the row carries this binary's digest", got.Source, got.Reason)
	}
}
