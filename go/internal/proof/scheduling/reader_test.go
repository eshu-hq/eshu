// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestReaderScheduleHasTwelveRequestsAndThirteenBarriers(t *testing.T) {
	var order []bool
	var barriers []int
	request := func(_ context.Context, candidate bool) (measuredRequest, error) {
		order = append(order, candidate)
		return measuredRequest{duration: time.Millisecond}, nil
	}
	checkpoint := func(_ context.Context, index int) error {
		barriers = append(barriers, index)
		return nil
	}
	validate := func(_ context.Context, _ int, _ bool, _ measuredRequest) error { return nil }
	if err := runReaderSchedule(context.Background(), request, checkpoint, validate); err != nil {
		t.Fatal(err)
	}
	if want := timingOrder(3); !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
	if len(barriers) != 13 {
		t.Fatalf("barriers=%v", barriers)
	}
	for index, barrier := range barriers {
		if barrier != index {
			t.Fatalf("barrier[%d]=%d", index, barrier)
		}
	}
}

type fakeReaderHealthRow struct {
	database   string
	systemID   string
	recovery   bool
	readOnly   string
	receiveLSN sql.NullString
	replayLSN  sql.NullString
	backlog    sql.NullInt64
	replayed   sql.NullTime
	sampled    time.Time
	err        error
}

func (row fakeReaderHealthRow) Scan(dst ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dst) == 9 {
		*dst[0].(*string) = row.database
		*dst[1].(*string) = row.systemID
		dst = dst[2:]
	}
	*dst[0].(*bool) = row.recovery
	*dst[1].(*string) = row.readOnly
	*dst[2].(*sql.NullString) = row.receiveLSN
	*dst[3].(*sql.NullString) = row.replayLSN
	*dst[4].(*sql.NullInt64) = row.backlog
	*dst[5].(*sql.NullTime) = row.replayed
	*dst[6].(*time.Time) = row.sampled
	return nil
}

type fakeWALReceiverRow struct {
	count    int64
	pid      sql.NullInt64
	status   string
	receipt  sql.NullTime
	hasStats bool
	now      time.Time
	err      error
}

func (row fakeWALReceiverRow) Scan(dst ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dst) == 6 {
		*dst[0].(*int64) = row.count
		*dst[1].(*sql.NullInt64) = row.pid
		*dst[2].(*sql.NullString) = sql.NullString{String: row.status, Valid: row.status != ""}
		*dst[3].(*sql.NullTime) = row.receipt
		*dst[4].(*bool) = row.hasStats
		*dst[5].(*time.Time) = row.now
		return nil
	}
	*dst[0].(*string) = row.status
	*dst[1].(*sql.NullTime) = row.receipt
	*dst[2].(*time.Time) = row.now
	return nil
}

type fakeReaderHealthQuery struct {
	row      fakeReaderHealthRow
	receiver fakeWALReceiverRow
	calls    int
	clears   int
	clearErr error
}

func (query *fakeReaderHealthQuery) QueryRow(_ context.Context, statement string, _ ...any) pgx.Row {
	query.calls++
	if strings.Contains(statement, "pg_stat_wal_receiver") {
		return query.receiver
	}
	return query.row
}

func (query *fakeReaderHealthQuery) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	if statement != "SELECT pg_stat_clear_snapshot()" {
		return pgconn.CommandTag{}, errors.New("unexpected stats reset")
	}
	query.clears++
	return pgconn.CommandTag{}, query.clearErr
}

func TestReaderHealthRequiresLiveReadOnlyStandby(t *testing.T) {
	now := time.Now()
	good := fakeReaderHealthRow{
		database: "eshu7033", systemID: "123456789",
		recovery: true, readOnly: "on", receiveLSN: sql.NullString{String: "0/124", Valid: true},
		replayLSN: sql.NullString{String: "0/123", Valid: true}, backlog: sql.NullInt64{Int64: 1, Valid: true},
		replayed: sql.NullTime{Time: now.Add(-time.Second), Valid: true}, sampled: now,
	}
	goodReceiver := fakeWALReceiverRow{count: 1, pid: sql.NullInt64{Int64: 257, Valid: true}, status: "streaming", receipt: sql.NullTime{Time: now.Add(-time.Second), Valid: true}, hasStats: true, now: now}
	limits := readerLagLimits{
		maxApplyBacklogBytes: 2, maxReplayAge: 2 * time.Second, maxReceiverMessageAge: 2 * time.Second,
		expectedDatabase: "eshu7033", expectedSystemID: "123456789", receiverMode: "strict_receiver", expectedReceiverPID: 257,
	}
	query := &fakeReaderHealthQuery{row: good, receiver: goodReceiver}
	if err := checkReaderHealth(context.Background(), query, 0, limits); err != nil {
		t.Fatal(err)
	}
	if query.calls != 2 || query.clears != 1 {
		t.Fatalf("health queries=%d clears=%d", query.calls, query.clears)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	query.calls = 0
	if err := checkReaderHealth(canceled, query, 0, limits); !errors.Is(err, context.Canceled) || query.calls != 0 {
		t.Fatalf("canceled health error=%v queries=%d", err, query.calls)
	}
	for _, tc := range []struct {
		name   string
		change func(*fakeReaderHealthRow)
	}{
		{"wrong database", func(row *fakeReaderHealthRow) { row.database = "postgres" }},
		{"wrong system", func(row *fakeReaderHealthRow) { row.systemID = "987654321" }},
		{"promoted", func(row *fakeReaderHealthRow) { row.recovery = false }},
		{"writable", func(row *fakeReaderHealthRow) { row.readOnly = "off" }},
		{"no receive LSN", func(row *fakeReaderHealthRow) { row.receiveLSN.Valid = false }},
		{"no replay LSN", func(row *fakeReaderHealthRow) { row.replayLSN.Valid = false }},
		{"apply backlog", func(row *fakeReaderHealthRow) { row.backlog.Int64 = 3 }},
		{"negative backlog", func(row *fakeReaderHealthRow) { row.backlog.Int64 = -1 }},
		{"no replay timestamp", func(row *fakeReaderHealthRow) { row.replayed.Valid = false }},
		{"old replay", func(row *fakeReaderHealthRow) { row.replayed.Time = now.Add(-3 * time.Second) }},
		{"future replay", func(row *fakeReaderHealthRow) { row.replayed.Time = now.Add(time.Second) }},
		{"query failure", func(row *fakeReaderHealthRow) { row.err = errors.New("probe failed") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := good
			tc.change(&row)
			if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{row: row, receiver: goodReceiver}, 1, limits); err == nil {
				t.Fatal("unsafe reader health accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*fakeWALReceiverRow)
	}{
		{"not streaming", func(row *fakeWALReceiverRow) { row.status = "stopped" }},
		{"no receipt", func(row *fakeWALReceiverRow) { row.receipt.Valid = false }},
		{"old message", func(row *fakeWALReceiverRow) { row.receipt.Time = now.Add(-3 * time.Second) }},
		{"receiver denied", func(row *fakeWALReceiverRow) { row.err = errors.New("permission denied") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver := goodReceiver
			tc.change(&receiver)
			if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{row: good, receiver: receiver}, 1, limits); err == nil {
				t.Fatal("unsafe WAL receiver accepted")
			}
		})
	}
	if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{
		row: good, receiver: goodReceiver, clearErr: errors.New("permission denied"),
	}, 1, limits); err == nil {
		t.Fatal("stats snapshot permission failure accepted")
	}
}

func TestReaderHealthExplicitRedactedReceiverMode(t *testing.T) {
	now := time.Now()
	health := fakeReaderHealthRow{
		database: "eshu7033", systemID: "123456789", recovery: true, readOnly: "on",
		receiveLSN: sql.NullString{String: "0/124", Valid: true},
		replayLSN:  sql.NullString{String: "0/123", Valid: true},
		backlog:    sql.NullInt64{Int64: 1, Valid: true},
		replayed:   sql.NullTime{Time: now.Add(-time.Millisecond), Valid: true}, sampled: now,
	}
	receiver := fakeWALReceiverRow{count: 1, pid: sql.NullInt64{Int64: 257, Valid: true}, now: now}
	limits := readerLagLimits{
		maxApplyBacklogBytes: 2, maxReplayAge: 2 * time.Second, maxReceiverMessageAge: 2 * time.Second,
		expectedDatabase: "eshu7033", expectedSystemID: "123456789",
		receiverMode: "redacted_receiver", expectedReceiverPID: 257,
	}
	if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{row: health, receiver: receiver}, 0, limits); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*fakeWALReceiverRow)
	}{
		{"absent PID", func(row *fakeWALReceiverRow) { row.pid.Valid = false }},
		{"zero PID", func(row *fakeWALReceiverRow) { row.pid.Int64 = 0 }},
		{"changed PID", func(row *fakeWALReceiverRow) { row.pid.Int64 = 258 }},
		{"absent row", func(row *fakeWALReceiverRow) { row.count = 0 }},
		{"multiple rows", func(row *fakeWALReceiverRow) { row.count = 2 }},
		{"new stats grant", func(row *fakeWALReceiverRow) { row.hasStats = true }},
		{"status visible", func(row *fakeWALReceiverRow) { row.status = "streaming" }},
		{"receipt visible", func(row *fakeWALReceiverRow) { row.receipt = sql.NullTime{Time: now, Valid: true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := receiver
			tc.change(&changed)
			if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{row: health, receiver: changed}, 1, limits); err == nil {
				t.Fatal("unsafe redacted WAL receiver accepted")
			}
		})
	}
	stale := health
	stale.receiveLSN = sql.NullString{String: "0/124", Valid: true}
	stale.replayLSN = stale.receiveLSN
	stale.backlog.Int64 = 0
	stale.replayed.Time = now.Add(-3 * time.Second)
	if err := checkReaderHealth(context.Background(), &fakeReaderHealthQuery{row: stale, receiver: receiver}, 1, limits); err == nil {
		t.Fatal("stale replay with equal LSNs accepted")
	}
}

func TestReaderRedactedPIDChangeStopsBeforeNextRequest(t *testing.T) {
	now := time.Now()
	health := fakeReaderHealthRow{
		database: "eshu7033", systemID: "123456789", recovery: true, readOnly: "on",
		receiveLSN: sql.NullString{String: "0/124", Valid: true},
		replayLSN:  sql.NullString{String: "0/123", Valid: true},
		backlog:    sql.NullInt64{Int64: 1, Valid: true},
		replayed:   sql.NullTime{Time: now.Add(-time.Millisecond), Valid: true}, sampled: now,
	}
	query := &fakeReaderHealthQuery{row: health, receiver: fakeWALReceiverRow{
		count: 1, pid: sql.NullInt64{Int64: 257, Valid: true}, now: now,
	}}
	limits := readerLagLimits{
		maxApplyBacklogBytes: 2, maxReplayAge: 2 * time.Second,
		expectedDatabase: "eshu7033", expectedSystemID: "123456789", receiverMode: "redacted_receiver", expectedReceiverPID: 257,
	}
	requests := 0
	request := func(_ context.Context, _ bool) (measuredRequest, error) {
		requests++
		return measuredRequest{duration: time.Millisecond}, nil
	}
	checkpoint := func(ctx context.Context, barrier int) error {
		if barrier == 2 {
			query.receiver.pid.Int64 = 258
		}
		return checkReaderHealth(ctx, query, barrier, limits)
	}
	validate := func(_ context.Context, _ int, _ bool, _ measuredRequest) error { return nil }
	if err := runReaderSchedule(context.Background(), request, checkpoint, validate); err == nil {
		t.Fatal("PID change accepted")
	}
	if requests != 2 {
		t.Fatalf("dispatched %d requests after PID changed", requests)
	}
}

func TestReaderCaseRejectsWrongConnectionCount(t *testing.T) {
	for _, count := range []int{0, 3, 5} {
		if err := runDynamicReaderCase(context.Background(), make([]*pgx.Conn, count), dynamicWorkload{}, readerLagLimits{}, readerResourceConfig{}); err == nil {
			t.Fatalf("accepted %d connections", count)
		}
	}
}

func TestReaderLagLimitsRequireExplicitBoundedValues(t *testing.T) {
	t.Setenv("ESHU7033_READER_HEALTH_MODE", "strict_receiver")
	t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", "257")
	t.Setenv("ESHU7033_MAX_APPLY_BACKLOG_BYTES", "1048576")
	t.Setenv("ESHU7033_MAX_REPLAY_AGE_MS", "5000")
	t.Setenv("ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS", "5000")
	limits, err := loadReaderLagLimits()
	if err != nil {
		t.Fatal(err)
	}
	if limits.maxApplyBacklogBytes != 1048576 || limits.maxReplayAge != 5*time.Second || limits.maxReceiverMessageAge != 5*time.Second {
		t.Fatalf("wrong parsed limits: %+v", limits)
	}
	for _, tc := range []struct{ name, key, value string }{
		{"missing backlog", "ESHU7033_MAX_APPLY_BACKLOG_BYTES", ""},
		{"negative backlog", "ESHU7033_MAX_APPLY_BACKLOG_BYTES", "-1"},
		{"oversized backlog", "ESHU7033_MAX_APPLY_BACKLOG_BYTES", "1099511627777"},
		{"noncanonical backlog", "ESHU7033_MAX_APPLY_BACKLOG_BYTES", "01"},
		{"zero replay age", "ESHU7033_MAX_REPLAY_AGE_MS", "0"},
		{"oversized replay age", "ESHU7033_MAX_REPLAY_AGE_MS", "300001"},
		{"missing message age", "ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := loadReaderLagLimits(); err == nil {
				t.Fatal("unsafe lag limit accepted")
			}
		})
	}
}

func TestReaderHealthModeRequiresExplicitSelection(t *testing.T) {
	t.Setenv("ESHU7033_MAX_APPLY_BACKLOG_BYTES", "1048576")
	t.Setenv("ESHU7033_MAX_REPLAY_AGE_MS", "5000")
	t.Setenv("ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS", "5000")
	t.Setenv("ESHU7033_READER_HEALTH_MODE", "redacted_receiver")
	t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", "257")
	limits, err := loadReaderLagLimits()
	if err != nil || limits.receiverMode != "redacted_receiver" || limits.expectedReceiverPID != 257 {
		t.Fatalf("explicit redacted mode: limits=%+v err=%v", limits, err)
	}
	for _, tc := range []struct{ mode, pid string }{
		{"", "257"},
		{"unknown", "257"},
		{"redacted_receiver", ""},
		{"redacted_receiver", "0"},
		{"redacted_receiver", "01"},
		{"redacted_receiver", "-1"},
		{"redacted_receiver", "2147483648"},
		{"strict_receiver", ""},
		{"strict_receiver", "0"},
	} {
		t.Run(tc.mode+"/"+tc.pid, func(t *testing.T) {
			t.Setenv("ESHU7033_READER_HEALTH_MODE", tc.mode)
			t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", tc.pid)
			if _, err := loadReaderLagLimits(); err == nil {
				t.Fatal("unsafe health mode accepted")
			}
		})
	}
	t.Setenv("ESHU7033_READER_HEALTH_MODE", "strict_receiver")
	t.Setenv("ESHU7033_EXPECTED_RECEIVER_PID", "257")
	if _, err := loadReaderLagLimits(); err != nil {
		t.Fatalf("strict mode rejected: %v", err)
	}
}

func TestReaderScheduleStopsBeforeNextDispatch(t *testing.T) {
	wantErr := errors.New("bad first candidate witness")
	requests := 0
	barriers := 0
	request := func(_ context.Context, _ bool) (measuredRequest, error) {
		requests++
		return measuredRequest{duration: time.Millisecond}, nil
	}
	checkpoint := func(_ context.Context, _ int) error {
		barriers++
		return nil
	}
	validate := func(_ context.Context, index int, _ bool, _ measuredRequest) error {
		if index == 1 {
			return wantErr
		}
		return nil
	}
	if err := runReaderSchedule(context.Background(), request, checkpoint, validate); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if requests != 2 || barriers != 2 {
		t.Fatalf("requests=%d barriers=%d after failed witness", requests, barriers)
	}
}

func TestReaderScheduleStopsOnHealthErrorAndCancellation(t *testing.T) {
	wantErr := errors.New("standby health failed")
	requests := 0
	request := func(_ context.Context, _ bool) (measuredRequest, error) {
		requests++
		return measuredRequest{}, nil
	}
	checkpoint := func(_ context.Context, index int) error {
		if index == 2 {
			return wantErr
		}
		return nil
	}
	validate := func(_ context.Context, _ int, _ bool, _ measuredRequest) error { return nil }
	if err := runReaderSchedule(context.Background(), request, checkpoint, validate); !errors.Is(err, wantErr) {
		t.Fatalf("health error=%v", err)
	}
	if requests != 2 {
		t.Fatalf("dispatched %d requests after barrier failure", requests)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	requests = 0
	if err := runReaderSchedule(canceled, request, checkpoint, validate); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
	if requests != 0 {
		t.Fatalf("dispatched %d canceled requests", requests)
	}
}
