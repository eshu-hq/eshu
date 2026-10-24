// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unread

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// cursorRow is one result row: the next-repository and lap-repository columns,
// nil for NULL.
type cursorRow struct{ next, lap *string }

// cursorRows is a result of cursor rows.
type cursorRows struct {
	values []cursorRow
	next   int
	err    error
}

func (r *cursorRows) Next() bool {
	r.next++
	return r.next <= len(r.values)
}

func (r *cursorRows) Scan(dest ...any) error {
	row := r.values[r.next-1]
	for i, value := range []*string{row.next, row.lap} {
		if value != nil {
			target := dest[i].(*sql.NullString)
			target.String, target.Valid = *value, true
		}
	}
	return nil
}
func (r *cursorRows) Err() error   { return r.err }
func (r *cursorRows) Close() error { return nil }

type cursorQueryer struct {
	rows  *cursorRows
	err   error
	query string
	args  []any
}

func (q *cursorQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.query, q.args = query, args
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

func text(v string) *string { return &v }

func TestCursorReaderReturnsTheNewestGenerationCursor(t *testing.T) {
	queryer := &cursorQueryer{rows: &cursorRows{values: []cursorRow{{next: text("acme/next"), lap: text("acme/last")}}}}
	got, err := NewCursorReader(queryer).FanoutCursor(context.Background(), "scope-1")
	if want := (securityalerts.FanoutCursor{NextRepository: "acme/next", LapRepository: "acme/last"}); err != nil || got != want {
		t.Fatalf("FanoutCursor() = %+v, %v; want %+v", got, err, want)
	}
	// The newest pending-or-active generation, with the ordering
	// activeGenerationFreshnessQuery uses, not ingestion_scopes.active_generation_id:
	// a claim between commit and activation must not read an older cursor.
	for _, want := range []string{
		"status IN ('pending', 'active')",
		"ORDER BY generation.ingested_at DESC, generation.generation_id DESC",
		"LIMIT 1",
		"security_alert_target_read_states",
	} {
		if !strings.Contains(queryer.query, want) {
			t.Errorf("cursor query is missing %q", want)
		}
	}
	if strings.Contains(queryer.query, "active_generation_id") {
		t.Error("cursor query reads ingestion_scopes.active_generation_id")
	}
	if len(queryer.args) != 1 || queryer.args[0] != "scope-1" {
		t.Errorf("cursor query args = %v, want the scope id", queryer.args)
	}
}

func TestCursorReaderStartsAtTheTopWhenNothingIsStored(t *testing.T) {
	cases := map[string]*cursorRows{
		"no generation":             {},
		"generation without a row":  {values: []cursorRow{{}}},
		"generation with no cursor": {values: []cursorRow{{next: text(""), lap: text("")}}},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NewCursorReader(&cursorQueryer{rows: rows}).FanoutCursor(context.Background(), "scope-1")
			if err != nil || got != (securityalerts.FanoutCursor{}) {
				t.Fatalf("FanoutCursor() = %+v, %v; want the top, no lap, and no error", got, err)
			}
		})
	}
}

func TestCursorReaderFailsInsteadOfFallingBackToTheTop(t *testing.T) {
	boom := errors.New("connection reset")
	if _, err := NewCursorReader(&cursorQueryer{err: boom}).FanoutCursor(context.Background(), "scope-1"); !errors.Is(err, boom) {
		t.Fatalf("query error = %v, want it wrapped", err)
	}
	rows := &cursorRows{values: []cursorRow{{next: text("x")}}, err: boom}
	if _, err := NewCursorReader(&cursorQueryer{rows: rows}).FanoutCursor(context.Background(), "scope-1"); !errors.Is(err, boom) {
		t.Fatalf("row error = %v, want it wrapped", err)
	}
}
