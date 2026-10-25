// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stateQueryer answers the secret-lines state read with one fixed row.
type stateQueryer struct {
	state string
	epoch int64
	none  bool
	err   error
}

func (q stateQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	return &stateRows{q: q}, nil
}

type stateRows struct {
	q    stateQueryer
	read bool
}

func (r *stateRows) Next() bool {
	if r.q.none || r.read {
		return false
	}
	r.read = true
	return true
}

func (r *stateRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.q.state
	*dest[1].(*int64) = r.q.epoch
	return nil
}
func (r *stateRows) Err() error   { return nil }
func (r *stateRows) Close() error { return nil }

// TestUnclaimedFinalizeIsSupersededOnlyWhenTheEpochMovedOn: bootstrap's
// Finalize claimed nothing. A newer epoch means a newer load owns the table
// (Warn, exit 0). The same epoch means someone else finalized this run's epoch
// or the claim failed for another reason: lost exclusivity, a non-nil error
// and an ERROR event. A missing state row is also an error.
func TestUnclaimedFinalizeIsSupersededOnlyWhenTheEpochMovedOn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		queryer stateQueryer
		wantErr string
		wantLog string
	}{
		{"newer epoch", stateQueryer{state: "not_built", epoch: 8}, "", "secret_lines.finalize_superseded"},
		{"same epoch", stateQueryer{state: "ready", epoch: 7}, "lost exclusivity", "secret_lines.finalize_lost_epoch"},
		{"older epoch", stateQueryer{state: "ready", epoch: 6}, "lost exclusivity", "secret_lines.finalize_lost_epoch"},
		{"no row", stateQueryer{none: true}, "no state row", "secret_lines.finalize_lost_epoch"},
		{"read fails", stateQueryer{err: errors.New("boom")}, "boom", "secret_lines.finalize_lost_epoch"},
	}
	for _, tc := range cases {
		var logs bytes.Buffer
		err := reconcileUnclaimedFinalize(context.Background(), tc.queryer, 7, slog.New(slog.NewJSONHandler(&logs, nil)))
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want nil", tc.name, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want one containing %q", tc.name, err, tc.wantErr)
		}
		if !strings.Contains(logs.String(), tc.wantLog) {
			t.Errorf("%s: logs = %s, want %s", tc.name, logs.String(), tc.wantLog)
		}
	}
}
