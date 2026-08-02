// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// containerImageIdentityAdmissionQuery is the begin-before-mutate admission
// check (#5874), mirroring awsCloudRuntimeDriftAdmissionQuery
// (aws_cloud_runtime_drift_admission.go, #5848/#5875 P1) for the
// container_image_identity domain. It is a conditional upsert against a
// one-row-per-(scope, generation) watermark table: a first pass for the pair
// always inserts and is admitted; a later pass is admitted only if ITS
// fencing token is at or after the stored one. A pass whose token is older
// than the stored one -- a stalled worker whose evidence read predates a
// fresher worker's completed write for the SAME scope and generation --
// updates zero rows, which the caller reads back as rejection BEFORE
// publishing or retiring anything.
//
// `<=`, not `<`: a retry or redelivery of a pass that reuses the SAME token
// must still be admitted.
//
// This is the piece the per-row conflict guard (reducerFactBatchInsertQuery)
// does not provide on its own. That guard only fires when two passes collide
// on the SAME fact_id; container_image_identity's fact id is keyed by
// (scope_id, generation_id, image_ref), outcome-independent (#5854), so a
// canonical decision and its own tombstone already collide there -- but a
// PASS that saw fewer or different image references than another (a
// reclassification whose old and new outcome-independent identity happen to
// differ is not reachable today since identity dropped outcome, yet the
// underlying race the admission table protects against is broader: this
// table orders WHOLE PASSES for a (scope, generation), not just individual
// colliding rows, which is what lets an admission-rejected pass be stopped
// before it writes anything at all, including facts that would not collide
// with any row a fresher pass wrote).
//
// # The fencing token is database-issued, not the reducer host's wall clock
//
// fencingToken comes from a Postgres SEQUENCE
// (container_image_identity_fencing_token_seq, migration 093), issued via
// ContainerImageIdentityFencingTokenIssuer at the same point in
// ContainerImageIdentityHandler.Handle where the wall-clock evidenceAsOf
// watermark used to be captured -- before the first fact load, not at
// write-commit time. See containerImageIdentityFencingToken's doc comment
// (container_image_identity_writer.go) for why a wall-clock token is unsound
// under cross-replica clock skew.
const containerImageIdentityAdmissionQuery = `
INSERT INTO container_image_identity_write_admission (
    scope_id, generation_id, fencing_token, updated_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
    fencing_token = EXCLUDED.fencing_token,
    updated_at    = EXCLUDED.updated_at
WHERE container_image_identity_write_admission.fencing_token <= EXCLUDED.fencing_token
`

// ContainerImageIdentityWriteSupersededFailureClass classifies a write whose
// evidence-read watermark is older than one this (scope, generation) pair
// already admitted, mirroring AWSCloudRuntimeDriftWriteSupersededFailureClass.
// The reducer queue treats it as a non-counting retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) so losing this
// normal race never erodes the retry budget or dead-letters the intent: a
// retry re-reads evidence with a fresh watermark and, absent a still-fresher
// concurrent pass, is admitted.
const ContainerImageIdentityWriteSupersededFailureClass = "container_image_identity_write_superseded"

// containerImageIdentityWriteSupersededError marks an admission-check
// rejection as retryable so the durable queue re-runs the intent, instead of
// a stalled pass's write landing unopposed after a fresher pass already
// committed.
type containerImageIdentityWriteSupersededError struct {
	scopeID      string
	generationID string
}

func (e containerImageIdentityWriteSupersededError) Error() string {
	return fmt.Sprintf(
		"container image identity write superseded: a fresher pass already admitted for scope %s generation %s",
		e.scopeID, e.generationID,
	)
}

func (containerImageIdentityWriteSupersededError) Retryable() bool { return true }

func (containerImageIdentityWriteSupersededError) FailureClass() string {
	return ContainerImageIdentityWriteSupersededFailureClass
}

// isContainerImageIdentityWriteSuperseded reports whether err is the
// admission-check rejection above, unwrapping through fmt.Errorf(%w) wrapping
// the way the writer returns it.
func isContainerImageIdentityWriteSuperseded(err error) bool {
	var superseded containerImageIdentityWriteSupersededError
	return errors.As(err, &superseded)
}

// tryAdmitContainerImageIdentityWrite runs the begin-before-mutate admission
// check and reports whether this pass may proceed to publish or retire. It
// must run BEFORE any other statement in the write's atomic unit -- the
// existing transaction for the pre-cutover and post-cutover-oversized-batch
// paths, or the sole statement for the empty (rows==0, no legacy rows) path,
// which has no transaction to begin because it otherwise issues nothing at
// all.
func tryAdmitContainerImageIdentityWrite(
	ctx context.Context,
	exec workloadIdentityExecer,
	scopeID string,
	generationID string,
	fencingToken int64,
	now time.Time,
) (bool, error) {
	result, err := exec.ExecContext(
		ctx,
		containerImageIdentityAdmissionQuery,
		scopeID,
		generationID,
		fencingToken,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("container image identity write admission: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("container image identity write admission rows affected: %w", err)
	}
	return affected > 0, nil
}

// ContainerImageIdentityFencingTokenIssuer supplies the database-issued,
// monotonically increasing fencing token the admission check and
// fact_records rows are stamped with (#5874), mirroring
// AWSCloudRuntimeDriftFencingTokenIssuer. Backed by a Postgres sequence
// (container_image_identity_fencing_token_seq, migration 093) in production
// -- see PostgresContainerImageIdentityFencingTokenIssuer
// (go/internal/storage/postgres/container_image_identity_fencing_token.go) --
// so the value reflects real cross-replica invocation order rather than any
// individual reducer host's wall clock.
type ContainerImageIdentityFencingTokenIssuer interface {
	// NextContainerImageIdentityFencingToken returns the next value in
	// issuance order. Never returns the same value twice.
	NextContainerImageIdentityFencingToken(ctx context.Context) (int64, error)
}

// errContainerImageIdentityMissingFencingToken is returned when a write
// reaches the writer with a zero FencingToken -- a caller that forgot to wire
// ContainerImageIdentityFencingTokenIssuer or bypassed
// ContainerImageIdentityHandler.Handle entirely. A legitimate Postgres
// sequence value is never 0 (ascending sequences default to MINVALUE 1), so
// zero unambiguously means "never issued", not "issued the value zero".
var errContainerImageIdentityMissingFencingToken = errors.New(
	"container image identity write requires a non-zero fencing_token: the admission check and the durable rows have no ordering value to be stamped with",
)

// validateContainerImageIdentityFencingToken rejects a write with no issued
// fencing token. Deliberately a hard error rather than defaulting to 0 or to
// this writer's own clock: a defaulted token would either tie every
// forgotten-wiring write together (the exact-tie hazard the sequence closes)
// or reintroduce the host-wall-clock vulnerability this change closes.
func validateContainerImageIdentityFencingToken(fencingToken int64) error {
	if fencingToken == 0 {
		return errContainerImageIdentityMissingFencingToken
	}
	return nil
}
