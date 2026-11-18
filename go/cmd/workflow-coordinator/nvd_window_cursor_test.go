// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// TestNVDWindowCursorReaderMapsStoreState proves the cmd-level reader hands the
// coordinator every field the cursor store scanned: the cursor it plans from
// and the head and update time it reports lag from (eshu-hq/eshu#10716).
func TestNVDWindowCursorReaderMapsStoreState(t *testing.T) {
	t.Parallel()

	updated := time.Date(2026, 11, 15, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name        string
		row         []any
		wantSet     bool
		wantCursor  int64
		wantHead    int64
		wantUpdated time.Time
	}{
		{
			name: "all columns present",
			row: []any{
				sql.NullInt64{Int64: 1767398400000000, Valid: true},
				sql.NullInt64{Int64: 1767484800000000, Valid: true},
				sql.NullTime{Time: updated, Valid: true},
			},
			wantSet:     true,
			wantCursor:  1767398400000000,
			wantHead:    1767484800000000,
			wantUpdated: updated,
		},
		{
			name: "NULL head and update time map to zero values",
			row: []any{
				sql.NullInt64{Int64: 1767398400000000, Valid: true},
				sql.NullInt64{},
				sql.NullTime{},
			},
			wantSet:    true,
			wantCursor: 1767398400000000,
		},
		{
			name: "NULL cursor maps to unset",
			row: []any{
				sql.NullInt64{},
				sql.NullInt64{Int64: 1767484800000000, Valid: true},
				sql.NullTime{Time: updated, Valid: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{tt.row}}}}
			got, err := nvdWindowCursorReader{database: database}.
				ReadNVDWindowCursor(context.Background(), "collector-nvd-rolling")
			if err != nil {
				t.Fatalf("ReadNVDWindowCursor() error = %v, want nil", err)
			}
			if got.Set != tt.wantSet ||
				got.CursorUnixMicro != tt.wantCursor ||
				got.HeadUnixMicro != tt.wantHead ||
				!got.UpdatedAt.Equal(tt.wantUpdated) {
				t.Fatalf(
					"ReadNVDWindowCursor() = %+v, want set=%t cursor=%d head=%d updated=%v",
					got, tt.wantSet, tt.wantCursor, tt.wantHead, tt.wantUpdated,
				)
			}
		})
	}
}

// TestNVDWindowCursorReaderNoRowIsUnset proves an absent row maps to the unset
// cursor so the coordinator bootstraps from the planner lookback instead of
// planning from zero.
func TestNVDWindowCursorReaderNoRowIsUnset(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{}}}
	got, err := nvdWindowCursorReader{database: database}.
		ReadNVDWindowCursor(context.Background(), "collector-nvd-rolling")
	if err != nil {
		t.Fatalf("ReadNVDWindowCursor() error = %v, want nil", err)
	}
	if got.Set || got.CursorUnixMicro != 0 || got.HeadUnixMicro != 0 || !got.UpdatedAt.IsZero() {
		t.Fatalf("ReadNVDWindowCursor() = %+v, want the zero unset cursor", got)
	}
}

// TestNVDWindowCursorReaderPropagatesProbeFailure proves a failed probe
// reaches the coordinator as an error and never as an unset cursor, so a
// database blip cannot re-plan the backlog over committed windows.
func TestNVDWindowCursorReaderPropagatesProbeFailure(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("connection reset by peer")
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: probeErr}}}
	got, err := nvdWindowCursorReader{database: database}.
		ReadNVDWindowCursor(context.Background(), "collector-nvd-rolling")
	if !errors.Is(err, probeErr) {
		t.Fatalf("ReadNVDWindowCursor() error = %v, want it to wrap %v", err, probeErr)
	}
	if got.Set {
		t.Fatalf("ReadNVDWindowCursor() = %+v, want the zero cursor alongside the error", got)
	}
}
