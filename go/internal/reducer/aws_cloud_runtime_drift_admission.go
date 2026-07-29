// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AWSCloudRuntimeDriftTx is the narrow transactional surface the drift writer
// needs: the insert-admission check, the versioned upsert, and the
// generation-authoritative retire commit atomically under one transaction.
// *sql.Tx satisfies it through AWSCloudRuntimeDriftSQLTx.
type AWSCloudRuntimeDriftTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

// AWSCloudRuntimeDriftBeginner opens a transaction for the drift write.
// go/internal/storage/postgres/aws_cloud_runtime_drift_admission_beginner.go
// provides the production adapter (AWSCloudRuntimeDriftAdmissionBeginner,
// wrapping the shared postgres.Beginner every other reducer writer that needs
// a transaction uses); that direction compiles because internal/storage/postgres
// already imports internal/reducer, not the reverse.
type AWSCloudRuntimeDriftBeginner interface {
	BeginAWSCloudRuntimeDriftTx(context.Context) (AWSCloudRuntimeDriftTx, error)
}

// awsCloudRuntimeDriftAdmissionQuery is the begin-before-mutate admission
// check (#5848). It is a conditional upsert against a one-row-per-(scope,
// generation) watermark table: a first pass for the pair always inserts and
// is admitted; a later pass is admitted only if ITS evidence-read watermark
// is at or after the stored one. A pass whose watermark is older than the
// stored one — a stalled worker whose evidence read predates a fresher
// worker's completed write for the SAME scope and generation — updates zero
// rows, which the caller reads back as rejection BEFORE issuing the insert or
// retire that follow in the same transaction.
//
// `<=`, not `<`, for the same reason reducerFactBatchInsertQuery's guard uses
// it: a retry or redelivery of the SAME pass carries the SAME watermark
// (evidence is read once), and `<` would reject its own retry.
//
// This is the piece the insert and retire fences do not provide on their own.
// The insert's conflict guard (reducerFactBatchInsertVersionedQuery) only
// fires when two passes collide on the same fact_id, and aws_cloud_runtime_drift's
// identity embeds finding_kind, so a stale pass that reclassifies an ARN mints
// a DIFFERENT fact_id and never collides. Without this admission check that
// pass's insert lands unopposed.
const awsCloudRuntimeDriftAdmissionQuery = `
INSERT INTO aws_cloud_runtime_drift_write_admission (
    scope_id, generation_id, fencing_token, updated_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
    fencing_token = EXCLUDED.fencing_token,
    updated_at    = EXCLUDED.updated_at
WHERE aws_cloud_runtime_drift_write_admission.fencing_token <= EXCLUDED.fencing_token
`

// AWSCloudRuntimeDriftWriteSupersededFailureClass classifies a write whose
// evidence-read watermark is older than one this (scope, generation) pair
// already admitted. The reducer queue treats it as a non-counting retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) so losing this
// normal race never erodes the retry budget or dead-letters the intent: a
// retry re-reads evidence with a fresh watermark and, absent a still-fresher
// concurrent pass, is admitted.
const AWSCloudRuntimeDriftWriteSupersededFailureClass = "aws_cloud_runtime_drift_write_superseded"

// errAWSCloudRuntimeDriftWriteSuperseded marks an admission-check rejection as
// retryable so the durable queue re-runs the intent, instead of a stalled
// pass's insert landing unopposed after a fresher pass already committed.
type awsCloudRuntimeDriftWriteSupersededError struct {
	scopeID      string
	generationID string
}

func (e awsCloudRuntimeDriftWriteSupersededError) Error() string {
	return fmt.Sprintf(
		"aws cloud runtime drift write superseded: a fresher pass already admitted for scope %s generation %s",
		e.scopeID, e.generationID,
	)
}

func (awsCloudRuntimeDriftWriteSupersededError) Retryable() bool { return true }

func (awsCloudRuntimeDriftWriteSupersededError) FailureClass() string {
	return AWSCloudRuntimeDriftWriteSupersededFailureClass
}

// tryAdmitAWSCloudRuntimeDriftWrite runs the begin-before-mutate admission
// check and reports whether this pass may proceed to insert/retire. It must
// run BEFORE any other statement in the write transaction.
func tryAdmitAWSCloudRuntimeDriftWrite(
	ctx context.Context,
	tx AWSCloudRuntimeDriftTx,
	scopeID string,
	generationID string,
	fencingToken int64,
	now time.Time,
) (bool, error) {
	result, err := tx.ExecContext(
		ctx,
		awsCloudRuntimeDriftAdmissionQuery,
		scopeID,
		generationID,
		fencingToken,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("aws cloud runtime drift write admission: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("aws cloud runtime drift write admission rows affected: %w", err)
	}
	return affected > 0, nil
}

// errAWSCloudRuntimeDriftMissingEvidenceAsOf is returned when a write reaches
// the writer without the evidence-read watermark the admission check and the
// durable rows are stamped with.
var errAWSCloudRuntimeDriftMissingEvidenceAsOf = errors.New(
	"aws cloud runtime drift write requires evidence_as_of: the admission check and the durable rows have no watermark to be stamped with",
)

// awsCloudRuntimeDriftFencingToken renders the write's evidence-read watermark
// as the BIGINT the admission table and fact_records.fencing_token both carry.
// Mirrors containerImageIdentityFencingToken (#5847): microsecond resolution,
// monotonic across reopens and retries without a durable counter.
func awsCloudRuntimeDriftFencingToken(evidenceAsOf time.Time) int64 {
	return evidenceAsOf.UTC().UnixMicro()
}

// validateAWSCloudRuntimeDriftFence rejects a write with no evidence-read
// watermark. Deliberately a hard error rather than a defaulted value: a zero
// EvidenceAsOf does not yield token 0 (time.Time{}.UnixMicro() is a large
// negative number every later pass's real watermark exceeds), so every row
// would rest at that floor and the admission guard would admit every later
// pass unconditionally -- the domain would look fenced while behaving
// unfenced. Defaulting to the writer's own clock would be worse: write time
// ranks a stalled worker highest, the exact inversion the watermark exists to
// prevent.
func validateAWSCloudRuntimeDriftFence(evidenceAsOf time.Time) error {
	if evidenceAsOf.IsZero() {
		return errAWSCloudRuntimeDriftMissingEvidenceAsOf
	}
	return nil
}
