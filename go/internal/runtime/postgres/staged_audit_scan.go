// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Lane C of the staged stale-winners audit scan (#10451): the recovery
// conflict classifier, the single-member tick runner, and the operator
// telemetry. The staged read phase runs census, every scope batch, and the
// stored-winners fetch on ONE pinned reader member, each as its own short
// transaction, so no cross-batch transaction holds a standby snapshot for
// minutes. Any batch error, timeout, or recovery conflict aborts the whole
// tick and discards staged state with no writer contact; the writer-side
// verify runs only after every read succeeds and stays authoritative.
//
// The legacy writer audit path is untouched: this file adds no writer SQL,
// writer pool, or writer-spanning transaction.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Staged-audit fail-closed bounds (#10451). A batch is one scope batch in its
// final narrow shape; staged state is the client-side per-key map the tick
// builds as batches stream in.
const (
	// DefaultStagedAuditBatchTimeout bounds one batch statement. The worst
	// production scope fetches in 2.7 s against a 30 s conflict window; 25 s
	// leaves headroom for a slow standby while failing a hung batch inside
	// the window the next tick retries.
	DefaultStagedAuditBatchTimeout = 25 * time.Second
	// StagedAuditMaxRowsPerBatch caps one scope batch. The census proves the
	// worst production scope holds 10,742 facts.
	StagedAuditMaxRowsPerBatch = 20000
	// StagedAuditMaxStagedFacts caps the whole tick's staged facts.
	StagedAuditMaxStagedFacts = 1000000
	// StagedAuditMaxDistinctKeys caps the whole tick's distinct staged keys.
	StagedAuditMaxDistinctKeys = 250000
)

// recoveryConflictPhrase is the binding half of the #10451 classifier. A
// recovery conflict is SQLSTATE 40001 AND this message; either alone is
// insufficient because 40001 also covers genuine serialization failures.
const recoveryConflictPhrase = "conflict with recovery"

// IsRecoveryConflict reports whether err is a standby recovery conflict: a
// 40001 carrying the recovery message. It unwraps pgconn errors by Code and
// falls back to the printed SQLSTATE for text forms such as the captured
// psql "ERROR:  40001: canceling statement due to conflict with recovery".
// A 57014 statement timeout never classifies as a conflict.
func IsRecoveryConflict(err error) bool {
	if err == nil {
		return false
	}
	return stagedAuditHasSQLState(err, "40001") && stagedAuditHasRecoveryPhrase(err)
}

// stagedAuditHasSQLState reports whether any layer carries the SQLSTATE: a
// pgconn PgError Code, or the printed state for text-form errors.
func stagedAuditHasSQLState(err error, state string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == state {
			return true
		}
	}
	for current := err; current != nil; {
		if strings.Contains(current.Error(), state) {
			return true
		}
		unwrapped, ok := current.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		current = unwrapped.Unwrap()
	}
	return false
}

// stagedAuditHasRecoveryPhrase reports whether any layer's message carries the
// recovery-conflict phrase, matched case-insensitively.
func stagedAuditHasRecoveryPhrase(err error) bool {
	for current := err; current != nil; {
		if strings.Contains(strings.ToLower(current.Error()), recoveryConflictPhrase) {
			return true
		}
		unwrapped, ok := current.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		current = unwrapped.Unwrap()
	}
	return false
}

// StagedAuditDeferralReason is the closed set behind the 3am deferral
// counter: budget (an over-budget batch tripped a fail-closed cap),
// conflict-exhausted (a recovery conflict aborted the tick), or batch-abort
// (any other batch failure, including timeouts).
type StagedAuditDeferralReason string

const (
	StagedAuditDeferBudget            StagedAuditDeferralReason = "budget"
	StagedAuditDeferConflictExhausted StagedAuditDeferralReason = "conflict_exhausted"
	StagedAuditDeferBatchAbort        StagedAuditDeferralReason = "batch_abort"
)

// StagedAuditOverBudgetError marks a batch or tick that tripped a fail-closed
// cap. It classifies as a budget deferral, never as a conflict.
type StagedAuditOverBudgetError struct {
	What  string
	Got   int
	Limit int
}

func (e *StagedAuditOverBudgetError) Error() string {
	return fmt.Sprintf("staged audit over budget: %s got %d, limit %d", e.What, e.Got, e.Limit)
}

// StagedAuditDeferredError aborts one audit tick with a closed reason. The
// tick defers and retries next interval; the error unwraps to the cause for
// logs, never for retry branching on unclassified errors.
type StagedAuditDeferredError struct {
	Reason StagedAuditDeferralReason
	Err    error
}

func (e *StagedAuditDeferredError) Error() string {
	return fmt.Sprintf("staged audit tick deferred (%s): %v", e.Reason, e.Err)
}

func (e *StagedAuditDeferredError) Unwrap() error { return e.Err }

// ClassifyStagedAuditError maps a tick failure to its deferral reason. A
// recovery conflict defers as conflict-exhausted, an over-budget batch as
// budget, and any other batch failure (including timeouts) as batch-abort.
func ClassifyStagedAuditError(err error) (StagedAuditDeferralReason, bool) {
	if err == nil {
		return "", false
	}
	var overBudget *StagedAuditOverBudgetError
	if errors.As(err, &overBudget) {
		return StagedAuditDeferBudget, true
	}
	if IsRecoveryConflict(err) {
		return StagedAuditDeferConflictExhausted, true
	}
	var deferred *StagedAuditDeferredError
	if errors.As(err, &deferred) {
		return deferred.Reason, true
	}
	return StagedAuditDeferBatchAbort, true
}

// StagedAuditCensusItem names one (scope, generation) pair enumerated by the
// tick census.
type StagedAuditCensusItem struct {
	ScopeID      string
	GenerationID string
}

// StagedAuditBatchResult counts what one scope batch staged.
type StagedAuditBatchResult struct {
	Facts        int
	DistinctKeys int
}

// StagedAuditTickPlan is the tick's read shape. Census, FetchBatch, and
// FetchStoredWinners run on the pinned reader member, each in its own short
// transaction; each callback receives the borrowed member connection for its
// statement and must not retain it. Verify is the writer-side check; the
// runner calls it only after every read succeeds, so an aborted tick never
// contacts the writer.
type StagedAuditTickPlan struct {
	Census             func(ctx context.Context, reader db.Queryer) ([]StagedAuditCensusItem, error)
	FetchBatch         func(ctx context.Context, reader db.Queryer, item StagedAuditCensusItem) (StagedAuditBatchResult, error)
	FetchStoredWinners func(ctx context.Context, reader db.Queryer) (int, error)
	Verify             func(ctx context.Context) (discarded int64, err error)
}

// StagedAuditTickResult carries the 3am surface of one completed tick.
type StagedAuditTickResult struct {
	MemberID         string
	Batches          int
	MaxBatchDuration time.Duration
	StagedFacts      int
	DistinctKeys     int
	StoredWinners    int
	ReplayLagStart   time.Duration
	ReplayLagEnd     time.Duration
	Discarded        int64
}

func (r StagedAuditTickResult) String() string {
	return fmt.Sprintf("staged audit tick on %s: %d batches, %d facts, %d keys",
		r.MemberID, r.Batches, r.StagedFacts, r.DistinctKeys)
}

// StagedAuditFleet is the member-pinned borrow surface the tick runner needs.
// A production implementation pins one qualified reader member for the tick
// and runs each statement in its own short transaction on that member,
// returning the connection to the pool between statements: it must never
// hold one cross-batch transaction or cursor that pins a standby snapshot.
type StagedAuditFleet interface {
	PinMember(ctx context.Context) (string, error)
	RunOnMember(ctx context.Context, member string, fn func(ctx context.Context, reader db.Queryer) error) error
	ReplayLag(ctx context.Context, member string) (time.Duration, error)
}

// StagedAuditRunnerOptions tunes the tick runner. Zero values select the
// production defaults.
type StagedAuditRunnerOptions struct {
	BatchTimeout time.Duration
}

// StagedAuditRunner runs one staged audit tick on a single pinned reader
// member. It holds no writer handle by construction: the only writer contact
// is the plan's Verify callback, invoked solely on the all-reads-succeeded
// path.
type StagedAuditRunner struct {
	fleet      StagedAuditFleet
	telemetry  *StagedAuditTelemetry
	batchLimit time.Duration
}

// NewStagedAuditRunner builds a tick runner over fleet, recording into
// telemetry (nil telemetry records nowhere).
func NewStagedAuditRunner(fleet StagedAuditFleet, telemetry *StagedAuditTelemetry, options StagedAuditRunnerOptions) *StagedAuditRunner {
	limit := options.BatchTimeout
	if limit <= 0 {
		limit = DefaultStagedAuditBatchTimeout
	}
	if telemetry == nil {
		telemetry = NewStagedAuditTelemetry()
	}
	return &StagedAuditRunner{fleet: fleet, telemetry: telemetry, batchLimit: limit}
}

// RunTick executes one staged audit tick: pin one member, run census, every
// scope batch, and the stored-winners fetch each in its own short
// transaction on that member, then verify. Any batch error, timeout, cap
// trip, or recovery conflict aborts the tick with a *StagedAuditDeferredError
// and discards staged state: partial diffs never reach the writer.
func (r *StagedAuditRunner) RunTick(ctx context.Context, plan StagedAuditTickPlan) (StagedAuditTickResult, error) {
	var none StagedAuditTickResult
	member, err := r.fleet.PinMember(ctx)
	if err != nil {
		return none, &StagedAuditDeferredError{Reason: StagedAuditDeferBatchAbort, Err: fmt.Errorf("pin staged audit member: %w", err)}
	}
	lagStart, err := r.fleet.ReplayLag(ctx, member)
	if err != nil {
		return none, r.abort(member, StagedAuditDeferBatchAbort, fmt.Errorf("read staged audit replay lag: %w", err))
	}
	var scopes []StagedAuditCensusItem
	if err := r.fleet.RunOnMember(ctx, member, func(stmtCtx context.Context, reader db.Queryer) error {
		var censusErr error
		scopes, censusErr = plan.Census(stmtCtx, reader)
		return censusErr
	}); err != nil {
		return none, r.abort(member, classifyStagedAuditAbort(err), fmt.Errorf("census staged audit scopes: %w", err))
	}
	result := StagedAuditTickResult{MemberID: member, ReplayLagStart: lagStart}
	for _, item := range scopes {
		batchCtx, cancel := context.WithTimeout(ctx, r.batchLimit)
		batchStarted := time.Now()
		var batch StagedAuditBatchResult
		batchErr := r.fleet.RunOnMember(batchCtx, member, func(stmtCtx context.Context, reader db.Queryer) error {
			var fetchErr error
			batch, fetchErr = plan.FetchBatch(stmtCtx, reader, item)
			return fetchErr
		})
		batchElapsed := time.Since(batchStarted)
		cancel()
		if batchErr != nil {
			return none, r.abort(member, classifyStagedAuditAbort(batchErr), fmt.Errorf("fetch staged audit batch: %w", batchErr))
		}
		if batch.Facts > StagedAuditMaxRowsPerBatch {
			return none, r.abort(member, StagedAuditDeferBudget, &StagedAuditOverBudgetError{What: "batch facts", Got: batch.Facts, Limit: StagedAuditMaxRowsPerBatch})
		}
		result.Batches++
		result.StagedFacts += batch.Facts
		result.DistinctKeys += batch.DistinctKeys
		if batchElapsed > result.MaxBatchDuration {
			result.MaxBatchDuration = batchElapsed
		}
		if result.StagedFacts > StagedAuditMaxStagedFacts {
			return none, r.abort(member, StagedAuditDeferBudget, &StagedAuditOverBudgetError{What: "staged facts", Got: result.StagedFacts, Limit: StagedAuditMaxStagedFacts})
		}
		if result.DistinctKeys > StagedAuditMaxDistinctKeys {
			return none, r.abort(member, StagedAuditDeferBudget, &StagedAuditOverBudgetError{What: "distinct keys", Got: result.DistinctKeys, Limit: StagedAuditMaxDistinctKeys})
		}
	}
	if err := r.fleet.RunOnMember(ctx, member, func(stmtCtx context.Context, reader db.Queryer) error {
		var fetchErr error
		result.StoredWinners, fetchErr = plan.FetchStoredWinners(stmtCtx, reader)
		return fetchErr
	}); err != nil {
		return none, r.abort(member, classifyStagedAuditAbort(err), fmt.Errorf("fetch staged audit stored winners: %w", err))
	}
	if plan.Verify != nil {
		discarded, err := plan.Verify(ctx)
		if err != nil {
			return none, r.abort(member, StagedAuditDeferBatchAbort, fmt.Errorf("verify staged audit winners: %w", err))
		}
		result.Discarded = discarded
		r.telemetry.ObserveVerify(discarded)
	}
	lagEnd, err := r.fleet.ReplayLag(ctx, member)
	if err != nil {
		return none, r.abort(member, StagedAuditDeferBatchAbort, fmt.Errorf("read staged audit replay lag: %w", err))
	}
	result.ReplayLagEnd = lagEnd
	r.telemetry.ObserveTick(result)
	return result, nil
}

// classifyStagedAuditAbort maps a statement failure to its deferral reason.
// Recovery conflicts exhaust the tick; everything else from a read statement
// (transport loss, timeouts, rejections) aborts the batch.
func classifyStagedAuditAbort(err error) StagedAuditDeferralReason {
	if IsRecoveryConflict(err) {
		return StagedAuditDeferConflictExhausted
	}
	if reason, ok := ClassifyStagedAuditError(err); ok {
		return reason
	}
	return StagedAuditDeferBatchAbort
}

// abort records the deferral and returns the typed tick error. Staged state
// lives only in the abandoned result value, so returning discards it.
func (r *StagedAuditRunner) abort(member string, reason StagedAuditDeferralReason, err error) *StagedAuditDeferredError {
	r.telemetry.ObserveDeferral(reason)
	return &StagedAuditDeferredError{Reason: reason, Err: fmt.Errorf("member %s: %w", member, err)}
}
