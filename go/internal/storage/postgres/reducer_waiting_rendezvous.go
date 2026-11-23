// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
