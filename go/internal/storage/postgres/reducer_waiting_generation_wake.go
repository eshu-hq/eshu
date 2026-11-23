// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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

// The park-versus-activation rendezvous (#11256).
//
// A generation-activation wait is parked in two autocommit statements (insert
// the wait row, then check whether it is already satisfied) while an
// activation commits its generation and runs its wake in one transaction.
// Without a rendezvous an activation whose wake statement ran before the wait
// row committed misses the row (it cannot see it), and the parker's check,
// which runs before that activation commits, misses the activation (it cannot
// see it either), so the wait sleeps to the audit or the backstop.
//
// Lock design. One advisory lock per provider scope, key
// activationRendezvousKey:
//
//   - The activation takes it exclusively, first, before any row lock, and
//     holds it until it commits (activateWithConsumerWake).
//   - A park takes it in shared mode between its insert and its satisfaction
//     check. A shared lock on key k granted at time T proves that every
//     activation of k that was in flight when the wait row committed has
//     finished by T, and that any activation of k that starts after T takes
//     its wake snapshot after the committed insert, so its wake sees the row.
//     The park holds a lock only inside one statement, and waits on at most
//     one key at a time: it holds no lock another scope's activation needs
//     while it waits. Two scopes' activations never wait on each other, and a
//     park never joins a lock cycle (an activation holds one advisory lock and
//     takes nothing of the parker's).
//
// A park first tries every key without waiting, in chunks of
// rendezvousTryChunkSize so one statement never holds more locks than the
// cluster lock table can take for it, and then waits on the keys that were
// held, one statement each. The whole rendezvous is bounded by
// narrowedParkRendezvousTimeout.

// narrowedParkRendezvousTimeout bounds how long a parking wait stands behind
// in-flight activations of its blockers, in total across keys. An activation
// holds its lock for one short transaction (its own wake statements are
// bounded by defaultActivationWakeLockTimeout), so a park only reaches this
// bound when the database is stalled; it then parks exactly as it did before
// the rendezvous existed, and the audit and the backstop bound the wake.
const narrowedParkRendezvousTimeout = 3 * time.Second

// rendezvousTryChunkSize is the most keys one non-blocking try statement
// locks. The locks of a statement are held until it ends, and every advisory
// lock takes a slot of the cluster-wide lock table (stock postgres: 64 per
// max_connections slot); one statement per blocking scope would fail with "out
// of shared memory" for a large blocking set, and that failure is cluster-wide.
const rendezvousTryChunkSize = 128

// rendezvousLogScopeSample caps how many provider scope IDs a rendezvous log
// line carries; a blocking set can be hundreds of scopes.
const rendezvousLogScopeSample = 5

// activationRendezvousKey names the advisory lock an activation of one scope
// holds from before it activates until it commits, and a park of a wait that
// names that scope as a blocker takes in shared mode. Keyed on the scope, not
// on a relationship_generations row, because the activation may INSERT the
// scope's first generation row, which a row lock cannot cover.
func activationRendezvousKey(scopeID string) string {
	return "relationship_generation_activation:" + scopeID
}

// activationRendezvousLockSQL is the activation side of the rendezvous: an
// exclusive transaction-level advisory lock on one scope, taken before the
// activation statement so the activator holds it across its activation, its
// wake statements, and its commit. $1 is activationRendezvousKey.
const activationRendezvousLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// tryBlockingActivationsQuery is the park side, fast path: a non-blocking
// shared advisory lock attempt on every key, returning the keys that were held
// exclusively (an in-flight activation). It evaluates every key, and as an
// autocommit statement releases what it took when it returns. $1 is the text[]
// of activationRendezvousKey values, at most rendezvousTryChunkSize.
const tryBlockingActivationsQuery = `
SELECT k
FROM unnest($1::text[]) AS k
WHERE NOT pg_try_advisory_xact_lock_shared(hashtextextended(k, 0))`

// awaitBlockingActivationQuery is the park side, contended path: a shared
// advisory lock on one key that waits behind that key's in-flight activation
// and returns as soon as it commits or rolls back. One key per statement, so
// the parker holds no lock while it waits. $1 is one activationRendezvousKey.
const awaitBlockingActivationQuery = `SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))`

// rendezvousScopeIDs lists the scopes whose in-flight activation a parking
// wait must stand behind: the blocking set of a narrowed wait, or the waiter's
// own scope for a coarse generation-activation wait (its provider is the
// activation of its own generation). Other wait kinds have other providers
// (a producer's acknowledgement, a phase publish, a winners rebuild) with their
// own transactions and lock orders, and take no rendezvous here.
func rendezvousScopeIDs(kind WaitingWakeKind, key waitingKey) []string {
	if kind != WaitingKindRelationshipGenerationActive {
		return nil
	}
	if len(key.BlockingScopeIDs) > 0 {
		return key.BlockingScopeIDs
	}
	if key.ScopeID != "" {
		return []string{key.ScopeID}
	}
	return nil
}

// awaitInFlightActivations closes the park-versus-activation window of a
// generation-activation wait (#11256) between the wait row's insert and its
// satisfaction check; see the lock design above.
//
// Only the kinds rendezvousScopeIDs names take the rendezvous. A park that
// finds no activation in flight costs one statement per
// rendezvousTryChunkSize keys and records nothing. One that has to wait
// records outcome waited on the park_rendezvous site.
//
// The wait row is already committed when the rendezvous runs, and a missed
// wake is a hint the audit (5 minutes) and the backstop (10 minutes) bound, so
// a rendezvous that fails to complete never fails the park: the bound firing
// records outcome timeout, any other statement error records outcome error,
// both log a warning and return nil, and the satisfaction check that follows
// still runs. Only the caller's own cancellation is returned (shutdown, not a
// rendezvous failure), as is an undecodable key (a bug, not a runtime state).
func awaitInFlightActivations(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	kind WaitingWakeKind,
	keyJSON string,
) error {
	var key waitingKey
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		return fmt.Errorf("decode waiting key: %w", err)
	}
	scopeIDs := rendezvousScopeIDs(kind, key)
	if len(scopeIDs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		keys = append(keys, activationRendezvousKey(scopeID))
	}
	waitCtx, cancel := context.WithTimeout(ctx, narrowedParkRendezvousTimeout)
	defer cancel()

	var busy []string
	for start := 0; start < len(keys); start += rendezvousTryChunkSize {
		held, err := tryBlockingActivations(waitCtx, database, keys[start:min(start+rendezvousTryChunkSize, len(keys))])
		if err != nil {
			return rendezvousFailed(ctx, waitCtx, instruments, scopeIDs, err)
		}
		busy = append(busy, held...)
	}
	if len(busy) == 0 {
		return nil
	}
	for _, busyKey := range busy {
		if _, err := database.ExecContext(waitCtx, awaitBlockingActivationQuery, busyKey); err != nil {
			return rendezvousFailed(ctx, waitCtx, instruments, scopeIDs, err)
		}
	}
	recordWaitingWake(ctx, instruments, telemetry.WaitingWakeSiteParkRendezvous, telemetry.WaitingWakeOutcomeWaited)
	return nil
}

// tryBlockingActivations runs one non-blocking try statement and returns the
// keys that were held exclusively.
func tryBlockingActivations(ctx context.Context, database db.ExecQueryer, keys []string) ([]string, error) {
	rows, err := database.QueryContext(ctx, tryBlockingActivationsQuery, array.StringArray(keys))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var busy []string
	for rows.Next() {
		var held string
		if err := rows.Scan(&held); err != nil {
			return nil, err
		}
		busy = append(busy, held)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return busy, nil
}

// rendezvousFailed turns a failed rendezvous statement into the park's
// outcome. The caller's own cancellation is returned. Everything else degrades
// to the pre-rendezvous behavior: the bound firing records timeout, any other
// error records error, and the park proceeds to its satisfaction check.
func rendezvousFailed(
	ctx, waitCtx context.Context,
	instruments *telemetry.Instruments,
	scopeIDs []string,
	err error,
) error {
	if ctx.Err() != nil {
		return fmt.Errorf("await in-flight activations: %w", err)
	}
	outcome, event := telemetry.WaitingWakeOutcomeError, "reducer_waiting.narrowed_park_rendezvous_error"
	if waitCtx.Err() != nil {
		outcome, event = telemetry.WaitingWakeOutcomeTimeout, "reducer_waiting.narrowed_park_rendezvous_timeout"
	}
	recordWaitingWake(ctx, instruments, telemetry.WaitingWakeSiteParkRendezvous, outcome)
	slog.Default().WarnContext(ctx, "waiting park could not finish its in-flight activation rendezvous; the audit bounds the wake",
		"event_name", event,
		"outcome", outcome,
		"provider_scope_count", len(scopeIDs),
		"provider_scope_ids_sample", scopeIDs[:min(rendezvousLogScopeSample, len(scopeIDs))],
		"timeout", narrowedParkRendezvousTimeout.String(),
		"error", err)
	return nil
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
// backstop still bound the miss. The reopened rows are stamped with the
// re-wake's own clock read, not the Ack's pre-transaction one: visible_at
// and reopened_at move to the wake time, as for the in-tx wake.
func (q ProjectorQueue) wakeAckWaitsAfterCommit(ctx context.Context, work projector.ScopeGenerationWork) {
	if q.skipAckPostCommitWake {
		return
	}
	q.wakeAckWaitsAfterCommitWithin(ctx, work, AckPostCommitWakeTimeout)
}

// wakeAckWaitsAfterCommitWithin is wakeAckWaitsAfterCommit with an explicit
// bound, so the timeout path is testable without a five-second wait.
func (q ProjectorQueue) wakeAckWaitsAfterCommitWithin(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	timeout time.Duration,
) {
	wakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	now := q.now()
	site := telemetry.WaitingWakeSiteAckPostCommit
	if err := wakeTerminalGenerationActiveWaits(wakeCtx, q.database, q.Instruments, site, now); err != nil {
		logPostCommitWakeError(context.WithoutCancel(ctx), "ack", work.Scope.ScopeID, "", err)
	}
	if err := wakeAckActivationWaits(wakeCtx, q.database, q.Instruments, site, work, now); err != nil {
		logPostCommitWakeError(context.WithoutCancel(ctx), "ack", work.Scope.ScopeID, work.Generation.GenerationID, err)
	}
}
