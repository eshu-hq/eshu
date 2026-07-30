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
// is admitted; a later pass is admitted only if ITS fencing token is at or
// after the stored one. A pass whose token is older than the stored one — a
// stalled worker whose evidence read predates a fresher worker's completed
// write for the SAME scope and generation — updates zero rows, which the
// caller reads back as rejection BEFORE issuing the insert or retire that
// follow in the same transaction.
//
// `<=`, not `<`, for the same reason reducerFactBatchInsertQuery's guard uses
// it: a retry or redelivery of a pass that reuses the SAME token (see the
// equal-token-retry case below) must still be admitted, and `<` would reject
// it.
//
// This is the piece the insert and retire fences do not provide on their own.
// The insert's conflict guard (reducerFactBatchInsertVersionedQuery) only
// fires when two passes collide on the same fact_id, and aws_cloud_runtime_drift's
// identity embeds finding_kind, so a stale pass that reclassifies an ARN mints
// a DIFFERENT fact_id and never collides. Without this admission check that
// pass's insert lands unopposed.
//
// # The fencing token is database-issued, not the reducer host's wall clock
//
// fencingToken comes from a Postgres SEQUENCE (aws_cloud_runtime_drift_fencing_token_seq,
// migration 089), issued via AWSCloudRuntimeDriftFencingTokenIssuer at the
// same point in AWSCloudRuntimeDriftHandler.Handle where the wall-clock
// evidenceAsOf watermark used to be captured -- before the evidence load, not
// at write-commit time. This closes a round-5 hostile review finding (#5875
// P1): the PRIOR token, evidenceAsOf.UnixMicro(), was the REDUCER HOST'S wall
// clock. With modest clock skew between reducer replicas, an older worker on
// a fast-clock host could carry a LARGER token than a later worker on a
// correct clock, so if the fresher worker committed first, the stale older
// worker was admitted afterward and its retire replaced correct truth with
// stale truth -- defeating this admission check's whole purpose. Since every
// reducer replica issues nextval() against the SAME shared Postgres instance,
// the value now reflects real invocation order, immune to any individual
// host's clock.
//
// The token is issued at EVIDENCE-READ time deliberately, not at write-commit
// time: a write-time token would order admission by COMMIT order instead of
// EVIDENCE RECENCY, which would silently reintroduce the ORIGINAL #5848 bug
// (a stalled worker's stale evidence landing after a fresher worker's
// committed write) while fixing the clock-skew one. A worker that reads
// evidence early but is slow to commit still carries the early token it was
// issued at read time, so a later reader that commits FIRST still correctly
// out-ranks it once the slow worker's commit finally lands.
//
// A useful side effect: because a Postgres sequence never returns the same
// value twice, the exact-watermark TIE this admission check used to have to
// tolerate (two independent passes stamping the identical wall-clock
// microsecond) is now categorically impossible -- every token is unique and
// strictly ordered by issuance, so "last-committer-wins on a tie" is no
// longer a real scenario this check has to reason about. The `<=` (not `<`)
// comparison still matters for the EQUAL-token case that remains possible: a
// caller that legitimately reuses the SAME already-issued token across a
// retry of the identical write attempt (see
// TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive) must
// still be admitted, not rejected as stale.
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

// AWSCloudRuntimeDriftFencingTokenIssuer supplies the database-issued,
// monotonically increasing fencing token the admission check and
// fact_records rows are stamped with (#5848, #5875 P1). Backed by a Postgres
// sequence (aws_cloud_runtime_drift_fencing_token_seq, migration 089) in
// production -- see PostgresAWSCloudRuntimeDriftFencingTokenIssuer
// (go/internal/storage/postgres/aws_cloud_runtime_drift_fencing_token.go) --
// so the value reflects real cross-replica invocation order rather than any
// individual reducer host's wall clock. See awsCloudRuntimeDriftAdmissionQuery's
// doc comment for why the caller (AWSCloudRuntimeDriftHandler.Handle) must
// call this at evidence-read time, not at write-commit time.
type AWSCloudRuntimeDriftFencingTokenIssuer interface {
	// NextAWSCloudRuntimeDriftFencingToken returns the next value in
	// issuance order. Never returns the same value twice.
	NextAWSCloudRuntimeDriftFencingToken(ctx context.Context) (int64, error)
}

// validateAWSCloudRuntimeDriftFence rejects a write with no evidence-read
// watermark. EvidenceAsOf no longer drives the fencing token (#5875 P1 moved
// that to AWSCloudRuntimeDriftWrite.FencingToken, issued by
// AWSCloudRuntimeDriftFencingTokenIssuer), but it remains a required
// audit/observability field: a zero value would mean Handle's readiness-bound
// elapsed-time check (aws_cloud_runtime_drift_readiness.go) and this write's
// own evidence-read timestamp are both unset, which should never happen
// against the real queue and is worth failing loudly on rather than silently
// defaulting.
func validateAWSCloudRuntimeDriftFence(evidenceAsOf time.Time) error {
	if evidenceAsOf.IsZero() {
		return errAWSCloudRuntimeDriftMissingEvidenceAsOf
	}
	return nil
}

// errAWSCloudRuntimeDriftMissingFencingToken is returned when a write reaches
// the writer with a zero FencingToken -- a caller that forgot to wire
// AWSCloudRuntimeDriftFencingTokenIssuer (or bypassed
// AWSCloudRuntimeDriftHandler.Handle entirely). A legitimate Postgres
// sequence value is never 0 (ascending sequences default to MINVALUE 1), so
// zero unambiguously means "never issued", not "issued the value zero".
var errAWSCloudRuntimeDriftMissingFencingToken = errors.New(
	"aws cloud runtime drift write requires a non-zero fencing_token: the admission check and the durable rows have no ordering value to be stamped with",
)

// validateAWSCloudRuntimeDriftFencingToken rejects a write with no issued
// fencing token. Deliberately a hard error rather than defaulting to 0 or to
// this writer's own clock: a defaulted token would either tie every
// forgotten-wiring write together (a silent return to the round-1 exact-tie
// hazard, for exactly the callers this validation exists to catch) or
// reintroduce the host-wall-clock vulnerability #5875 P1 closed.
func validateAWSCloudRuntimeDriftFencingToken(fencingToken int64) error {
	if fencingToken == 0 {
		return errAWSCloudRuntimeDriftMissingFencingToken
	}
	return nil
}
