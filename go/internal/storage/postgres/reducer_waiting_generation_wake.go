// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// terminalGenerationActiveWaitKeysQuery lists the parked generation_active
// wait keys whose generation already reached terminal truth (superseded by
// a newer generation or retired by a terminal projector Fail). The probe is
// scope-free (#11016): one claim sweep retires generations in every scope
// it scans, so scoping to the claimed scope misses the sweep's other
// victims. The predicate leads with waits_for_kind on
// reducer_waiting_items_wake_idx and the parked-waits table holds only
// deferred intents, so the widened probe stays a cheap index probe; see the
// #11016 evidence note for the measured plan.
const terminalGenerationActiveWaitKeysQuery = `
SELECT DISTINCT wait.waits_for_key
FROM reducer_waiting_items AS wait
JOIN scope_generations AS gen
  ON gen.scope_id = wait.scope_id AND gen.generation_id = wait.generation_id
WHERE wait.waits_for_kind = 'generation_active'
  AND gen.status IN ('superseded', 'failed')
`

// wakeTerminalGenerationActiveWaits reopens the parked generation_active
// waits whose generation already resolved terminally
// (#11015, widened #11016). The projector Claim sweep retires superseded
// generations in the same statement that claims the newer work, so the
// sweep's victims never see an activation wake; without this post-claim
// wake they sit out the 10-minute backstop even though their handler could
// already observe the terminal generation. Like every #10876 wake this is a
// hint: the claim gate and the handler re-check, so a concurrent park or a
// spurious match only re-parks. A waiter that parks after this wake is
// covered by the set-aside re-check, which shares the terminal predicate
// below. A probe failure is metered here (the per-key wakes meter
// themselves inside wakeWaitingByKey) so every failure path counts exactly
// once on eshu_dp_reducer_waiting_wake_total{outcome=error}. site names the
// wake site for telemetry: in_tx inside a providing save or after a Claim,
// ack_post_commit for the #11228 re-wake after the Ack committed.
func wakeTerminalGenerationActiveWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	now time.Time,
) error {
	rows, err := database.QueryContext(ctx, terminalGenerationActiveWaitKeysQuery)
	if err != nil {
		recordWaitingWake(ctx, instruments, site, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("list terminal generation waits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			recordWaitingWake(ctx, instruments, site, telemetry.WaitingWakeOutcomeError)
			return fmt.Errorf("scan terminal generation wait key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		recordWaitingWake(ctx, instruments, site, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("list terminal generation waits: %w", err)
	}
	for _, key := range keys {
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, site,
			WaitingKindGenerationActive, key, now); err != nil {
			return err
		}
	}
	return nil
}

// wakeTerminalGenerationActiveWaitsBestEffort runs the terminal-wait probe
// after a projector commit (Claim, including an empty sweep). A failure is
// logged and metered, never returned (#11016 F3): the claim already
// committed, so failing here would orphan the claimed work and misread a
// probe 40P01 as a claim conflict. The audit and the backstop still bound
// a missed wake.
func wakeTerminalGenerationActiveWaitsBestEffort(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	op, scopeID string,
	now time.Time,
) {
	if err := wakeTerminalGenerationActiveWaits(ctx, database, instruments, WaitingWakeSiteInTx, now); err != nil {
		logPostCommitWakeError(ctx, op, scopeID, "", err)
	}
}

// wakeGenerationActiveWaitsBestEffort reopens one scope generation's parked
// generation_active waits after its terminal save committed (Fail,
// heartbeat supersede). A failure is logged and metered, never returned
// (#11016 F3): the generation already resolved, so the caller keeps its
// committed outcome. The audit and the backstop still bound a missed wake.
func wakeGenerationActiveWaitsBestEffort(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	op, scopeID, generationID string,
	now time.Time,
) {
	if err := wakeGenerationActiveWaits(ctx, database, instruments, WaitingWakeSiteInTx,
		scopeID, generationID, now); err != nil {
		logPostCommitWakeError(ctx, op, scopeID, generationID, err)
	}
}

// logPostCommitWakeError records a post-commit generation_active wake
// failure at ERROR: the hint never fired, but the terminal truth stands and
// the audit plus the 10-minute backstop still bound the wait. Scope and
// generation ride along only when the caller names them (an empty sweep
// probes every scope).
func logPostCommitWakeError(ctx context.Context, op, scopeID, generationID string, err error) {
	attrs := []any{
		slog.String("queue", "projector"),
		slog.String("op", op),
		slog.String("error", err.Error()),
	}
	if scopeID != "" {
		attrs = append(attrs, slog.String(telemetry.LogKeyScopeID, scopeID))
	}
	if generationID != "" {
		attrs = append(attrs, slog.String(telemetry.LogKeyGenerationID, generationID))
	}
	attrs = append(attrs, slog.String(telemetry.LogKeyOutcome, telemetry.WaitingWakeOutcomeError))
	slog.ErrorContext(ctx,
		"projector post-commit generation_active wake failed; the audit and backstop still bound the miss",
		attrs...)
}

// AckPostCommitWakeTimeout bounds the projector Ack's post-commit re-wake
// (#11228). The re-wake runs detached from the Ack caller's cancellation, so
// this is its only bound. A healthy re-wake is a few index probes on
// reducer_waiting_items_wake_idx that reopen nobody; five seconds covers a
// wait behind a concurrent wake's row locks while staying far below the
// 5-minute audit interval, which reopens anything a timed-out re-wake
// missed. It matches PostCommitExactPhaseWakeTimeout (#11104).
const AckPostCommitWakeTimeout = 5 * time.Second

// wakeAckActivationWaits reopens the waits one projector Ack's activation
// provides: the generation_active waits parked on the acked scope
// generation (#10876) and the producer-terminal waits keyed by the producer
// domains the scope's collector kind publishes (#11000). The producer wake
// is load-bearing: the producer's reducer row commits before its generation
// activates, so the domain wait parked at reducer-Ack time only converges on
// an activation. An unmapped kind wakes nothing. site names the wake site
// for telemetry.
func wakeAckActivationWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	work projector.ScopeGenerationWork,
	now time.Time,
) error {
	if err := wakeGenerationActiveWaits(ctx, database, instruments, site,
		work.Scope.ScopeID, work.Generation.GenerationID, now); err != nil {
		return err
	}
	for _, producer := range producerDomainsForCollectorKind(string(work.Scope.CollectorKind)) {
		domainKey := waitingProducerDomainKey(string(producer))
		if domainKey == "" {
			continue
		}
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, site,
			WaitingKindProducerTerminal, domainKey, now); err != nil {
			return fmt.Errorf("wake producer terminal waits: %w", err)
		}
	}
	return nil
}

// wakeAckWaitsAfterCommit re-runs the Ack's wakes after its transaction
// committed (#11228). The park side of a wait writes first and reads second:
// failIntent commits the wait row, then re-checks the provider in a new
// statement. The in-tx wakes read before the Ack commits, so a waiter that
// parks inside the Ack's open window is seen by neither side. Reading again
// after the commit closes that window: under read committed each statement
// sees every transaction committed before it starts, so if the park's
// re-check missed the activation (the Ack committed after the re-check
// started), the wait row committed before that re-check and therefore
// before this re-wake starts.
//
// It re-runs the terminal-generation probe too, because the Ack's
// maintenance supersedes older generations in the same window. It runs on
// the pool connection, never on the committed transaction, detached from the
// caller's cancellation and bounded by AckPostCommitWakeTimeout. The wakes
// are idempotent: a woken wait row is deleted, so a re-wake of a row the
// in-tx wake or the park re-check already reopened is a no-op. A failure or
// timeout is logged at ERROR and metered (outcome=error), never returned:
// the generation is already active, and the audit plus the 10-minute
// backstop still bound the miss.
func (q ProjectorQueue) wakeAckWaitsAfterCommit(ctx context.Context, work projector.ScopeGenerationWork, now time.Time) {
	q.wakeAckWaitsAfterCommitWithin(ctx, work, now, AckPostCommitWakeTimeout)
}

// wakeAckWaitsAfterCommitWithin is wakeAckWaitsAfterCommit with an explicit
// bound, so the timeout path is testable without a five-second wait.
func (q ProjectorQueue) wakeAckWaitsAfterCommitWithin(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	now time.Time,
	timeout time.Duration,
) {
	wakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	site := telemetry.WaitingWakeSiteAckPostCommit
	if err := wakeTerminalGenerationActiveWaits(wakeCtx, q.database, q.Instruments, site, now); err != nil {
		logPostCommitWakeError(context.WithoutCancel(ctx), "ack", work.Scope.ScopeID, "", err)
	}
	if err := wakeAckActivationWaits(wakeCtx, q.database, q.Instruments, site, work, now); err != nil {
		logPostCommitWakeError(context.WithoutCancel(ctx), "ack", work.Scope.ScopeID, work.Generation.GenerationID, err)
	}
}
