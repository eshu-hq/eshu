// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Reader access metric names are stable operator-facing instruments. Their
// dimensions use only the closed role, stage, outcome, and pool state sets.
const (
	MetricReaderStageDuration      = "eshu_dp_postgres_reader_stage_duration_seconds"
	MetricReaderPoolConnections    = "eshu_dp_postgres_reader_pool_connections"
	MetricReaderPoolWaits          = "eshu_dp_postgres_reader_pool_waits_total"
	MetricReaderPoolWaitTime       = "eshu_dp_postgres_reader_pool_wait_duration_seconds"
	MetricReaderMemberQualified    = "eshu_dp_postgres_reader_member_qualified"
	MetricReaderMemberConnections  = "eshu_dp_postgres_reader_member_connections"
	MetricReaderMemberReservations = "eshu_dp_postgres_reader_member_reservations"
	MetricReaderMemberWaiters      = "eshu_dp_postgres_reader_member_waiters"
	MetricReaderMemberAttempts     = "eshu_dp_postgres_reader_member_attempts_total"
)

const (
	readerAttributeRole          = "role"
	readerAttributeStage         = "stage"
	readerAttributeOutcome       = "outcome"
	readerAttributeState         = "state"
	readerAttributeMemberOrdinal = "member_ordinal"
	readerUnknown                = "unknown"
	readerAccessSpanName         = "postgres.reader_access"
	readerQueryStartEventName    = "postgres.reader_query_start"
	readerQueryPIDKey            = "postgres.backend.pid"
	readerQueryRemoteKey         = "postgres.backend.remote"
	readerQueryRoleKey           = "postgres.role"
	readerQueryIdentityKey       = "postgres.backend.identity"
	readerQuerySequenceKey       = "postgres.query.sequence"
)

// StatusSnapshotSpanName identifies the bounded, one-transaction status read.
const StatusSnapshotSpanName = "postgres.status_snapshot"

const (
	statusSnapshotPhaseKey   = "phase"
	statusSnapshotOutcomeKey = "outcome"
)
