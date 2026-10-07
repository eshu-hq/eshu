// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const readerHealthDeadline = 2 * time.Second

type readerLagLimits struct {
	maxApplyBacklogBytes  int64
	maxReplayAge          time.Duration
	maxReceiverMessageAge time.Duration
	expectedDatabase      string
	expectedSystemID      string
	receiverMode          string
	expectedReceiverPID   int64
}

func parseReaderLimit(name string, maximum int64, allowZero bool) (int64, error) {
	raw := os.Getenv(name)
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || strconv.FormatInt(value, 10) != raw || value > maximum || value < 0 || (!allowZero && value == 0) {
		return 0, fmt.Errorf("%s requires a bounded canonical decimal integer", name)
	}
	return value, nil
}

func loadReaderLagLimits() (readerLagLimits, error) {
	backlog, err := parseReaderLimit("ESHU7033_MAX_APPLY_BACKLOG_BYTES", 1<<40, true)
	if err != nil {
		return readerLagLimits{}, err
	}
	replayMS, err := parseReaderLimit("ESHU7033_MAX_REPLAY_AGE_MS", 300000, false)
	if err != nil {
		return readerLagLimits{}, err
	}
	receiverMS, err := parseReaderLimit("ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS", 300000, false)
	if err != nil {
		return readerLagLimits{}, err
	}
	mode := os.Getenv("ESHU7033_READER_HEALTH_MODE")
	var receiverPID int64
	switch mode {
	case "strict_receiver", "redacted_receiver":
		receiverPID, err = parseReaderLimit("ESHU7033_EXPECTED_RECEIVER_PID", 1<<31-1, false)
		if err != nil {
			return readerLagLimits{}, err
		}
	default:
		return readerLagLimits{}, fmt.Errorf("ESHU7033_READER_HEALTH_MODE must be strict_receiver or redacted_receiver")
	}
	return readerLagLimits{
		maxApplyBacklogBytes: backlog, maxReplayAge: time.Duration(replayMS) * time.Millisecond,
		maxReceiverMessageAge: time.Duration(receiverMS) * time.Millisecond,
		receiverMode:          mode, expectedReceiverPID: receiverPID,
	}, nil
}

type readerHealthQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type readerRecoveryFailureReason string

const (
	readerMissingReceiveLSN      readerRecoveryFailureReason = "missing_receive_lsn"
	readerMissingReplayLSN       readerRecoveryFailureReason = "missing_replay_lsn"
	readerMissingApplyBacklog    readerRecoveryFailureReason = "missing_apply_backlog"
	readerNegativeApplyBacklog   readerRecoveryFailureReason = "negative_apply_backlog"
	readerApplyBacklogCeiling    readerRecoveryFailureReason = "apply_backlog_ceiling"
	readerMissingReplayTimestamp readerRecoveryFailureReason = "missing_replay_timestamp"
	readerFutureReplayTimestamp  readerRecoveryFailureReason = "future_replay_timestamp"
	readerReplayAgeCeiling       readerRecoveryFailureReason = "replay_age_ceiling"
)

type readerRecoveryHealthError struct {
	reason         readerRecoveryFailureReason
	applyBacklog   sql.NullInt64
	replayAgeMS    sql.NullInt64
	receiveLSNGood bool
	replayLSNGood  bool
}

func (failure readerRecoveryHealthError) Error() string {
	backlog := "NULL"
	if failure.applyBacklog.Valid {
		backlog = strconv.FormatInt(failure.applyBacklog.Int64, 10)
	}
	age := "NULL"
	if failure.replayAgeMS.Valid {
		age = strconv.FormatInt(failure.replayAgeMS.Int64, 10)
	}
	return fmt.Sprintf("reader recovery health reason=%s apply_backlog_bytes=%s replay_age_ms=%s receive_lsn_valid=%t replay_lsn_valid=%t",
		failure.reason, backlog, age, failure.receiveLSNGood, failure.replayLSNGood)
}

func diagnoseReaderRecoveryHealth(receiveLSN, replayLSN sql.NullString, backlog sql.NullInt64,
	replayed sql.NullTime, sampledAt time.Time, limits readerLagLimits,
) error {
	receiveLSNGood := receiveLSN.Valid && receiveLSN.String != ""
	replayLSNGood := replayLSN.Valid && replayLSN.String != ""
	failure := readerRecoveryHealthError{
		applyBacklog: backlog, receiveLSNGood: receiveLSNGood, replayLSNGood: replayLSNGood,
	}
	if replayed.Valid {
		failure.replayAgeMS = sql.NullInt64{Int64: sampledAt.Sub(replayed.Time).Milliseconds(), Valid: true}
	}
	switch {
	case !receiveLSNGood:
		failure.reason = readerMissingReceiveLSN
	case !replayLSNGood:
		failure.reason = readerMissingReplayLSN
	case !backlog.Valid:
		failure.reason = readerMissingApplyBacklog
	case backlog.Int64 < 0:
		failure.reason = readerNegativeApplyBacklog
	case backlog.Int64 > limits.maxApplyBacklogBytes:
		failure.reason = readerApplyBacklogCeiling
	case !replayed.Valid:
		failure.reason = readerMissingReplayTimestamp
	case replayed.Time.After(sampledAt):
		failure.reason = readerFutureReplayTimestamp
	case sampledAt.Sub(replayed.Time) > limits.maxReplayAge:
		failure.reason = readerReplayAgeCeiling
	default:
		return nil
	}
	return failure
}

func checkReaderHealth(ctx context.Context, tx readerHealthQuery, barrier int, limits readerLagLimits) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reader health context: %w", err)
	}
	healthCtx, cancel := context.WithTimeout(ctx, readerHealthDeadline)
	defer cancel()
	var recovery bool
	var actualDatabase, actualSystemID string
	var readOnly string
	var receiveLSN, replayLSN sql.NullString
	var backlog sql.NullInt64
	var replayed sql.NullTime
	var sampledAt time.Time
	const recoveryQuery = `SELECT current_database(), (SELECT system_identifier::text FROM pg_control_system()),
		pg_is_in_recovery(), current_setting('transaction_read_only'),
		pg_last_wal_receive_lsn()::text, pg_last_wal_replay_lsn()::text,
		pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn())::bigint,
		pg_last_xact_replay_timestamp(), clock_timestamp()`
	if err := tx.QueryRow(healthCtx, recoveryQuery).Scan(&actualDatabase, &actualSystemID, &recovery, &readOnly, &receiveLSN, &replayLSN,
		&backlog, &replayed, &sampledAt); err != nil {
		return fmt.Errorf("read standby recovery health: %w", err)
	}
	if err := validateFixedDatabase(limits.expectedDatabase, actualDatabase); err != nil {
		return err
	}
	if err := validateExpectedSystemID(limits.expectedSystemID); err != nil || actualSystemID != limits.expectedSystemID {
		return fmt.Errorf("reader health PostgreSQL system identity changed or is invalid")
	}
	if err := requireReadOnlyReader(recovery, readOnly); err != nil {
		return err
	}
	if err := diagnoseReaderRecoveryHealth(receiveLSN, replayLSN, backlog, replayed, sampledAt, limits); err != nil {
		return err
	}
	if _, err := tx.Exec(healthCtx, "SELECT pg_stat_clear_snapshot()"); err != nil {
		return fmt.Errorf("clear cached reader statistics: %w", err)
	}
	var receiverCount int64
	var receiverPID sql.NullInt64
	var receiverStatus sql.NullString
	var receipt sql.NullTime
	var hasStats bool
	var receiverNow time.Time
	const receiverQuery = `SELECT count(*)::bigint, max(pid)::bigint, max(status), max(last_msg_receipt_time),
		pg_has_role(current_user, 'pg_read_all_stats', 'USAGE'), clock_timestamp()
		FROM pg_stat_wal_receiver`
	if err := tx.QueryRow(healthCtx, receiverQuery).Scan(&receiverCount, &receiverPID, &receiverStatus, &receipt, &hasStats, &receiverNow); err != nil {
		return fmt.Errorf("read WAL receiver health: %w", err)
	}
	if receiverCount != 1 || !receiverPID.Valid || receiverPID.Int64 <= 0 {
		return fmt.Errorf("WAL receiver row or PID is missing or ambiguous")
	}
	if receiverPID.Int64 != limits.expectedReceiverPID {
		return fmt.Errorf("pinned WAL receiver PID changed")
	}
	switch limits.receiverMode {
	case "strict_receiver":
		if !receiverStatus.Valid || receiverStatus.String != "streaming" || !receipt.Valid || receipt.Time.After(receiverNow) ||
			receiverNow.Sub(receipt.Time) > limits.maxReceiverMessageAge {
			return fmt.Errorf("WAL receiver is missing, stale, or not streaming")
		}
		fmt.Printf("reader_health_barrier=%d receive_lsn=%s replay_lsn=%s apply_backlog_bytes=%d replay_age_ms=%d receiver_message_age_ms=%d receiver_mode=strict_receiver receiver_pid=%d\n",
			barrier, receiveLSN.String, replayLSN.String, backlog.Int64,
			sampledAt.Sub(replayed.Time).Milliseconds(), receiverNow.Sub(receipt.Time).Milliseconds(), receiverPID.Int64)
	case "redacted_receiver":
		if hasStats || receiverStatus.Valid || receipt.Valid {
			return fmt.Errorf("redacted WAL receiver visibility or pinned PID changed")
		}
		fmt.Printf("reader_health_barrier=%d receive_lsn=%s replay_lsn=%s apply_backlog_bytes=%d replay_age_ms=%d receiver_mode=redacted_receiver receiver_pid=%d receiver_message_age_ms=REDACTED\n",
			barrier, receiveLSN.String, replayLSN.String, backlog.Int64,
			sampledAt.Sub(replayed.Time).Milliseconds(), receiverPID.Int64)
	default:
		return fmt.Errorf("reader health mode is not selected")
	}
	return nil
}
