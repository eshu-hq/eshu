// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Ack marks one claimed projector work item as succeeded and publishes its
// generation. It never revives a superseded generation (#7130): the work item
// is then marked superseded and Ack returns failure.ErrWorkSuperseded, which
// callers treat like a superseded heartbeat.
func (q ProjectorQueue) Ack(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	_ runtime.Result,
) (err error) {
	if err := q.validate(); err != nil {
		return err
	}
	// A lock timeout rolled the transaction back, so nothing changed and the
	// attempt still owns the work; the caller renews the lease and retries.
	defer func() {
		if isPostgresLockNotAvailable(err) {
			err = fmt.Errorf("%w: %w", failure.ErrWorkAckDeferred, err)
		}
	}()

	beginner, ok := q.database.(db.Beginner)
	if !ok {
		return errors.New("projector queue database must support Begin for ack")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ack projector work: begin: %w", err)
	}
	txDone := false
	defer func() {
		if !txDone {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", q.ackLockTimeoutSetting()); err != nil {
		return fmt.Errorf("ack projector work: set lock timeout: %w", err)
	}
	now := q.now()
	// Lock order (#7389). The heartbeat supersede takes no lock it can wait on
	// (scope, work, generation, all SKIP LOCKED), so it cannot join a wait cycle.
	// Every blocking path takes the scope row first: Ack (scope, work, other
	// generations, target generation), Fail (scope, work, generation) and the
	// ingestion commit (scope, generation, work). The claim sweep, Ack's obsolete
	// maintenance, and the write-start marker take the scope's fence row SKIP
	// LOCKED (#7187, #7819) before any generation row and skip a busy scope, so no
	// two retire one scope's generations together. The delta-baseline refusal
	// takes no scope row and shares work-then-generation with Ack.
	if _, err := tx.ExecContext(ctx, updateProjectorScopeGenerationQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		return fmt.Errorf("ack projector work: update scope active generation: %w", err)
	}
	ackResult, err := tx.ExecContext(ctx, ackProjectorWorkItemQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner, work.AttemptCount)
	if err != nil {
		return fmt.Errorf("ack projector work: mark work succeeded: %w", err)
	}
	ackRows, err := ackResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("ack projector work: rows affected: %w", err)
	}
	if ackRows != 1 {
		return fmt.Errorf("ack projector work: %w", ErrProjectorClaimRejected)
	}
	// #7319: its own statement, after the scope lock returned, before any
	// statement that changes another generation (see deltaBaselineFenceQuery).
	baselineFence, err := checkAckDeltaBaseline(ctx, tx, work)
	if err != nil {
		return err
	}
	if baselineFence.outcome.Refused() {
		txDone = true // refuseDeltaBaselineAck rolls the transaction back.
		return q.refuseDeltaBaselineAck(ctx, tx, work, baselineFence, now)
	}
	steps := []struct {
		query string
		op    string
		args  []any
	}{
		{
			query: supersedeProjectorObsoleteGenerationsQuery,
			op:    "supersede obsolete terminal generations",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
		{
			query: supersedeProjectorActiveGenerationQuery,
			op:    "supersede active generation",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
	}
	prior := ""
	for _, step := range steps {
		if q.ChangedSinceJournalEnabled && step.query == supersedeProjectorActiveGenerationQuery {
			if prior, err = supersedeActiveGenerationReturningPrior(ctx, tx, step.args); err != nil {
				return fmt.Errorf("ack projector work: %s: %w", step.op, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("ack projector work: %s: %w", step.op, err)
		}
	}
	var activated bool
	journalOutcome := ""
	if q.ChangedSinceJournalEnabled {
		activated, journalOutcome, err = q.activateAndJournalAckGeneration(ctx, tx, work, now, prior)
	} else {
		activated, err = q.activateAckGeneration(ctx, tx, work, now)
	}
	if err != nil {
		return err
	}
	if !activated {
		txDone = true // refuseSupersededAck rolls the transaction back.
		return q.refuseSupersededAck(ctx, tx, work, now)
	}
	if err := activation.Insert(ctx, tx, work.Scope.ScopeID, work.Generation.GenerationID, projectorWorkItemID(work.Scope.ScopeID, work.Generation.GenerationID)); err != nil {
		return fmt.Errorf("ack projector work: %w", err)
	}
	// #7635: owe a producer-activation obligation for the activated
	// generation; the settle retires generations without producer evidence.
	if err := oweProducerActivationObligation(ctx, tx, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		return fmt.Errorf("ack projector work: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ack projector work: commit: %w", err)
	}
	txDone = true
	projector.RecordDeltaBaselineFence(ctx, q.Instruments, projector.DeltaBaselinePhaseAck, baselineFence.outcome)
	q.recordAckJournal(ctx, work, journalOutcome)

	q.runCrossplaneRedriveHook(ctx, work)
	q.runConfigStateDriftTriggerHook(ctx, work)

	return nil
}

// Journal outcomes of a flag-on Ack that activated its generation (#7127
// PR-3b).
const (
	// ackJournalOutcomeJournaled: the Ack wrote the generation's 'ack' row.
	ackJournalOutcomeJournaled = "journaled"
	// ackJournalOutcomeExists: the gate was acquired and the generation
	// already had a row (a re-ack, or a sweeper or backfill row).
	ackJournalOutcomeExists = "exists"
	// ackJournalOutcomeGateSkipped: a journal pass held its key, so the Ack
	// wrote no row and did not wait; the sweeper journals the generation.
	ackJournalOutcomeGateSkipped = "gate_skipped"
)

// supersedeActiveGenerationReturningPrior runs the RETURNING form of Ack's
// active supersede and returns the prior of the 'ack' activation row: the
// superseded generation when exactly one was active, otherwise "" (NULL).
func supersedeActiveGenerationReturningPrior(ctx context.Context, tx db.Transaction, args []any) (string, error) {
	rows, err := tx.QueryContext(ctx, supersedeProjectorActiveGenerationReturningQuery, args...)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var superseded []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		superseded = append(superseded, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(superseded) != 1 {
		return "", nil
	}
	return superseded[0], nil
}

// activateAndJournalAckGeneration is activateAckGeneration with the 'ack'
// activation row (activateAndJournalProjectorGenerationQuery). It reports
// whether the generation was activated and, if so, the journal outcome. It
// adds one advisory lock, a try-lock on the shared side of the journal key,
// so Ack still waits on nothing it did not wait on before.
func (q ProjectorQueue) activateAndJournalAckGeneration(
	ctx context.Context,
	tx db.Transaction,
	work projector.ScopeGenerationWork,
	now time.Time,
	prior string,
) (bool, string, error) {
	query := activateAndJournalProjectorGenerationQuery
	if q.ackJournalActivateQuery != "" {
		query = q.ackJournalActivateQuery
	}
	rows, err := tx.QueryContext(ctx, query, now, work.Scope.ScopeID, work.Generation.GenerationID, prior)
	if err != nil {
		return false, "", fmt.Errorf("ack projector work: activate and journal target generation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var activated, journaled int64
	var gated bool
	if rows.Next() {
		if err := rows.Scan(&activated, &journaled, &gated); err != nil {
			return false, "", fmt.Errorf("ack projector work: activate and journal target generation: scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, "", fmt.Errorf("ack projector work: activate and journal target generation: %w", err)
	}
	switch {
	case activated == 0 && journaled == 0:
		return false, "", nil
	case activated == 1 && journaled == 1 && gated:
		return true, ackJournalOutcomeJournaled, nil
	case activated == 1 && journaled == 0 && gated:
		return true, ackJournalOutcomeExists, nil
	case activated == 1 && journaled == 0:
		return true, ackJournalOutcomeGateSkipped, nil
	}
	return false, "", fmt.Errorf("ack projector work: activate and journal target generation: activated=%d journaled=%d gated=%t",
		activated, journaled, gated)
}

// recordAckJournal counts a committed flag-on Ack's journal outcome on
// eshu_dp_changed_since_ack_journal_total, the exact signal: every gate skip
// counts. The gate-skip INFO line is a sampled audit line: the first skip in
// the process logs, then at most one per ackSkipLogInterval, each with the
// scope, generation and outcome of the skip it logs and
// gate_skipped_since_last_log, the skips it did not log since the last line.
// An empty outcome (journal off) records nothing.
func (q ProjectorQueue) recordAckJournal(ctx context.Context, work projector.ScopeGenerationWork, outcome string) {
	if outcome == "" {
		return
	}
	if q.Instruments != nil && q.Instruments.ChangedSinceAckJournal != nil {
		q.Instruments.ChangedSinceAckJournal.Add(context.WithoutCancel(ctx), 1,
			metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	}
	if outcome != ackJournalOutcomeGateSkipped {
		return
	}
	sampler := q.ackSkipLog
	if sampler == nil {
		sampler = processAckSkipLog
	}
	logged, suppressed := sampler.admit()
	if !logged {
		return
	}
	slog.InfoContext(ctx, "changed-since ack journal skipped while a journal pass held its key; the sweeper journals the generation",
		slog.String(telemetry.LogKeyScopeID, work.Scope.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, work.Generation.GenerationID),
		slog.String(telemetry.LogKeyOutcome, outcome),
		slog.Int64("gate_skipped_since_last_log", suppressed),
	)
}

// ackSkipLogInterval bounds how often the Ack's gate-skip line logs in one
// process. During a bootstrap burst every Ack that lands while a journal pass
// holds the key skips, so a line per skip would flood.
const ackSkipLogInterval = time.Minute

// processAckSkipLog samples the gate-skip line for every ProjectorQueue in
// the process; ProjectorQueue is copied by value, so the state lives here.
var processAckSkipLog = newLogSampler(ackSkipLogInterval, time.Now)

// logSampler admits one log line per interval and counts the lines it
// suppressed in between. It is safe for concurrent use.
type logSampler struct {
	interval time.Duration
	now      func() time.Time

	mu         sync.Mutex
	last       time.Time
	suppressed int64
}

func newLogSampler(interval time.Duration, now func() time.Time) *logSampler {
	return &logSampler{interval: interval, now: now}
}

// admit reports whether this event logs and, when it does, how many events
// it suppressed since the last line it admitted.
func (s *logSampler) admit() (bool, int64) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() && now.Sub(s.last) < s.interval {
		s.suppressed++
		return false, 0
	}
	suppressed := s.suppressed
	s.last = now
	s.suppressed = 0
	return true, suppressed
}

// CrossplaneRedriveSweeper is the narrow surface ProjectorQueue.Ack calls to
// re-drive cross-scope Claim SATISFIED_BY correlations (issue #5476).
// crossplanestore.CrossplaneSatisfiedByRedriveSweeper implements it.
type CrossplaneRedriveSweeper interface {
	Sweep(ctx context.Context, xrdScopeID, xrdGenerationID string) (crossplanestore.CrossplaneRedriveSweepResult, error)
}

// runCrossplaneRedriveHook calls the wired CrossplaneRedriveSweeper AFTER
// Ack's own transaction has committed -- deliberately outside that
// transaction and on its own error path, mirroring the rejected design's
// review finding (issue #5476): the sweep's target-discovery fan-out is
// unbounded work relative to Ack's own fixed six-statement generation
// activation, so running it inline would turn every activation into a
// variable-length transaction holding Ack's row locks for however long the
// cross-scope sweep takes, including the failure mode where a stale lease
// commits under lock contention.
//
// A hook failure is deliberately never returned to the caller: by the time
// this runs, the generation is already correctly activated (Ack committed),
// and that real, valid work must not be reverted or reported as failed
// because a best-effort cross-scope re-drive attempt errored. The sweep
// itself is durably resumable (CrossplaneRedriveStateStore's claim/lease),
// so a failed attempt here is recovered by a later live trigger or the
// startup/periodic catch-up sweep, not by retrying Ack.
func (q ProjectorQueue) runCrossplaneRedriveHook(ctx context.Context, work projector.ScopeGenerationWork) {
	if q.CrossplaneRedrive == nil {
		return
	}
	if _, err := q.CrossplaneRedrive.Sweep(ctx, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		slog.ErrorContext(
			ctx, "crossplane cross-scope satisfied-by redrive sweep failed",
			"scope_id", work.Scope.ScopeID,
			"generation_id", work.Generation.GenerationID,
			"error", err,
		)
		if q.Instruments != nil && q.Instruments.CrossplaneRedriveSweeps != nil {
			q.Instruments.CrossplaneRedriveSweeps.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrOutcome("sweep_error"),
			))
		}
	}
}
