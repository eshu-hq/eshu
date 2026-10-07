// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReaderRecoveryFailureIdentifiesSanitizedOperand(t *testing.T) {
	now := time.Date(2026, time.October, 7, 1, 0, 0, 0, time.UTC)
	base := fakeReaderHealthRow{
		database: "eshu7033", systemID: "123456789", recovery: true, readOnly: "on",
		receiveLSN: sql.NullString{String: "SECRET_RECEIVE_LSN", Valid: true},
		replayLSN:  sql.NullString{String: "SECRET_REPLAY_LSN", Valid: true},
		backlog:    sql.NullInt64{Int64: 1, Valid: true},
		replayed:   sql.NullTime{Time: now.Add(-time.Second), Valid: true}, sampled: now,
	}
	limits := readerLagLimits{
		maxApplyBacklogBytes: 2, maxReplayAge: 2 * time.Second,
		expectedDatabase: "eshu7033", expectedSystemID: "123456789", receiverMode: "strict_receiver",
	}
	for _, tc := range []struct {
		name                string
		reason              string
		change              func(*fakeReaderHealthRow)
		wantBacklog         string
		wantAge             string
		wantReceiveLSNValid string
		wantReplayLSNValid  string
	}{
		{"missing receive LSN", "missing_receive_lsn", func(row *fakeReaderHealthRow) { row.receiveLSN.Valid = false }, "1", "1000", "false", "true"},
		{"empty receive LSN", "missing_receive_lsn", func(row *fakeReaderHealthRow) { row.receiveLSN.String = "" }, "1", "1000", "false", "true"},
		{"missing replay LSN", "missing_replay_lsn", func(row *fakeReaderHealthRow) { row.replayLSN.Valid = false }, "1", "1000", "true", "false"},
		{"empty replay LSN", "missing_replay_lsn", func(row *fakeReaderHealthRow) { row.replayLSN.String = "" }, "1", "1000", "true", "false"},
		{"missing backlog", "missing_apply_backlog", func(row *fakeReaderHealthRow) { row.backlog.Valid = false }, "NULL", "1000", "true", "true"},
		{"negative backlog", "negative_apply_backlog", func(row *fakeReaderHealthRow) { row.backlog.Int64 = -1 }, "-1", "1000", "true", "true"},
		{"large backlog", "apply_backlog_ceiling", func(row *fakeReaderHealthRow) { row.backlog.Int64 = 3 }, "3", "1000", "true", "true"},
		{"missing timestamp", "missing_replay_timestamp", func(row *fakeReaderHealthRow) { row.replayed.Valid = false }, "1", "NULL", "true", "true"},
		{"future timestamp", "future_replay_timestamp", func(row *fakeReaderHealthRow) { row.replayed.Time = now.Add(time.Millisecond) }, "1", "-1", "true", "true"},
		{"stale timestamp", "replay_age_ceiling", func(row *fakeReaderHealthRow) { row.replayed.Time = now.Add(-3 * time.Second) }, "1", "3000", "true", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := base
			tc.change(&row)
			query := &fakeReaderHealthQuery{row: row}
			err := checkReaderHealth(context.Background(), query, 3, limits)
			if err == nil {
				t.Fatal("unsafe recovery health accepted")
			}
			var failure readerRecoveryHealthError
			if !errors.As(err, &failure) || string(failure.reason) != tc.reason {
				t.Fatalf("untyped or wrong failure: %T %v", err, err)
			}
			for _, want := range []string{
				"reason=" + tc.reason,
				"apply_backlog_bytes=" + tc.wantBacklog,
				"replay_age_ms=" + tc.wantAge,
				"receive_lsn_valid=" + tc.wantReceiveLSNValid,
				"replay_lsn_valid=" + tc.wantReplayLSNValid,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "SECRET_") {
				t.Errorf("error leaks LSN content: %q", err)
			}
			if query.calls != 1 || query.clears != 0 {
				t.Errorf("unsafe state continued: calls=%d clears=%d", query.calls, query.clears)
			}
		})
	}
}

func TestStrictReceiverRequiresPinnedPIDAtEveryBarrier(t *testing.T) {
	now := time.Now()
	health := fakeReaderHealthRow{
		database: "eshu7033", systemID: "123456789", recovery: true, readOnly: "on",
		receiveLSN: sql.NullString{String: "0/124", Valid: true},
		replayLSN:  sql.NullString{String: "0/123", Valid: true},
		backlog:    sql.NullInt64{Int64: 1, Valid: true},
		replayed:   sql.NullTime{Time: now.Add(-time.Millisecond), Valid: true}, sampled: now,
	}
	receiver := fakeWALReceiverRow{
		count: 1, pid: sql.NullInt64{Int64: 257, Valid: true}, status: "streaming",
		receipt: sql.NullTime{Time: now.Add(-time.Millisecond), Valid: true}, hasStats: true, now: now,
	}
	limits := readerLagLimits{
		maxApplyBacklogBytes: 2, maxReplayAge: 2 * time.Second,
		maxReceiverMessageAge: 2 * time.Second, expectedDatabase: "eshu7033",
		expectedSystemID: "123456789", receiverMode: "strict_receiver", expectedReceiverPID: 257,
	}
	query := &fakeReaderHealthQuery{row: health, receiver: receiver}
	if err := checkReaderHealth(context.Background(), query, 0, limits); err != nil {
		t.Fatal(err)
	}
	query.receiver.pid.Int64 = 258
	if err := checkReaderHealth(context.Background(), query, 1, limits); err == nil {
		t.Fatal("strict receiver accepted a different PID at the next barrier")
	}
}

func TestStrictReceiverLoadsPreflightPID(t *testing.T) {
	t.Setenv("ESHU7033_READER_HEALTH_MODE", "strict_receiver")
	t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", "257")
	t.Setenv("ESHU7033_MAX_APPLY_BACKLOG_BYTES", "1048576")
	t.Setenv("ESHU7033_MAX_REPLAY_AGE_MS", "5000")
	t.Setenv("ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS", "5000")
	limits, err := loadReaderLagLimits()
	if err != nil || limits.expectedReceiverPID != 257 {
		t.Fatalf("strict receiver did not load pinned PID: limits=%+v err=%v", limits, err)
	}
	t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", "")
	if _, err := loadReaderLagLimits(); err == nil {
		t.Fatal("strict receiver accepted an absent preflight PID")
	}
}
