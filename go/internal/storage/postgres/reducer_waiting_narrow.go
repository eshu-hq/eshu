// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Per-item read-set waits (#10877): the narrowed waiting key and its wake.
//
// The three corpus consumers park on WaitingKindRelationshipGenerationActive
// with an extended JSONB key carrying the verdict-time blocking scope set and
// the read set (key-shape move, no schema change). The wake is a hint: an
// activation reopens waits naming the activated scope in their blocking set,
// and the retried handler re-checks the restricted fence. The audit and the
// 10-minute backstop bound a missed wake exactly like the coarse waits.

// waitingKey is the JSON waits_for_key per kind. BlockingScopeIDs and RepoIDs
// are set only on narrowed corpus waits; coarse waits omit them.
type waitingKey struct {
	ScopeID          string   `json:"scope_id,omitempty"`
	GenerationID     string   `json:"generation_id,omitempty"`
	ProducerDomain   string   `json:"producer_domain,omitempty"`
	EnqueuedAt       string   `json:"enqueued_at,omitempty"`
	Domains          []string `json:"domains,omitempty"`
	BlockingScopeIDs []string `json:"blocking_scope_ids,omitempty"`
	RepoIDs          []string `json:"repo_ids,omitempty"`
	// Keyspace and Phase are set only on phase-exact phase_committed waits
	// (#11104); coarse phase waits omit them, so the coarse Upsert wake's
	// JSONB-equality match never reaches an exact key.
	Keyspace string `json:"keyspace,omitempty"`
	Phase    string `json:"phase,omitempty"`
}

// corpusWaitNarrowing is the Fail-path contract for a deferral that parks on
// a per-item wait. Defined structurally so callers never import the reducer
// root: any deferral error carrying the verdict-time sets satisfies it.
type corpusWaitNarrowing interface {
	CorpusWaitRepoIDs() []string
	CorpusWaitBlockingScopeIDs() []string
}

// narrowedWaitingKey renders the narrowed corpus waits_for key for one parked
// intent: the waiter's own scope and generation plus the verdict-time
// blocking scope set and read set the activation wake matches on.
func narrowedWaitingKey(scopeID, generationID string, blockingScopeIDs, repoIDs []string) (string, bool) {
	scope := strings.TrimSpace(scopeID)
	gen := strings.TrimSpace(generationID)
	if scope == "" || gen == "" {
		return "", false
	}
	blocking := normalizeWaitingScopeIDs(blockingScopeIDs)
	if len(blocking) == 0 {
		return "", false
	}
	raw, err := json.Marshal(waitingKey{
		ScopeID:          scope,
		GenerationID:     gen,
		BlockingScopeIDs: blocking,
		RepoIDs:          normalizeWaitingScopeIDs(repoIDs),
	})
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// normalizeWaitingScopeIDs trims, drops empties and duplicates, and sorts,
// so a wait key rendered from two equal sets compares equal.
func normalizeWaitingScopeIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

// narrowedWaitingKeyForCause extracts the narrowed wait key from a deferral
// cause. It reports false when the cause carries no narrowing (global path)
// or the key cannot render, and the caller keeps the coarse wait.
func narrowedWaitingKeyForCause(
	intent contract.Intent,
	entry WaitingDeferral,
	cause error,
) (string, bool) {
	if entry.WaitsForKind != WaitingKindRelationshipGenerationActive {
		return "", false
	}
	var narrowing corpusWaitNarrowing
	if cause == nil || !errors.As(cause, &narrowing) {
		return "", false
	}
	return narrowedWaitingKey(intent.ScopeID, intent.GenerationID,
		narrowing.CorpusWaitBlockingScopeIDs(), narrowing.CorpusWaitRepoIDs())
}

// setAsideWaitingForCause parks one deferred intent, preferring the narrowed
// corpus wait when the deferral cause carries the verdict-time sets. Like
// setAsideWaiting it reopens at once when the provider already committed, so
// a waiter that registers after its provider never strands.
func setAsideWaitingForCause(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intent contract.Intent,
	entry WaitingDeferral,
	cause error,
	producerDomains []contract.Domain,
	now time.Time,
) (bool, error) {
	// Counting and not-a-wait classes never park, narrowed or not: the
	// guard lives here as well as in setAsideWaiting so a future
	// narrowing-carrying counting class cannot slip into the set-aside.
	if entry.WakeSite == WaitingWakeSiteCounting || entry.WakeSite == WaitingWakeSiteNotAWait {
		return false, nil
	}
	if keyJSON, ok := narrowedWaitingKeyForCause(intent, entry, cause); ok {
		return setAsideWaitingForKey(ctx, database, instruments, intent, entry, keyJSON, now)
	}
	return setAsideWaiting(ctx, database, instruments, intent, entry, producerDomains, now)
}

// setAsideWaitingForKey parks one wait row for an explicit waits_for key and,
// when the provider already committed, reopens it at once.
func setAsideWaitingForKey(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intent contract.Intent,
	entry WaitingDeferral,
	keyJSON string,
	now time.Time,
) (bool, error) {
	if err := insertWaitingItem(ctx, database, intent, entry, keyJSON, now); err != nil {
		return false, err
	}
	if err := awaitInFlightActivations(ctx, database, instruments, entry.WaitsForKind, keyJSON); err != nil {
		return true, err
	}
	satisfied, err := waitingSatisfiedForIntent(ctx, database, intent, entry, keyJSON, now)
	if err != nil {
		return true, err
	}
	if satisfied {
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, WaitingWakeSiteInTx,
			entry.WaitsForKind, keyJSON, now); err != nil {
			return true, err
		}
	}
	return true, nil
}

// narrowedParkRendezvousTimeout bounds how long a parking wait stands behind an
// in-flight activation of one of its blockers. An activation holds its lock for
// one short transaction (its own wake statements are bounded by
// defaultActivationWakeLockTimeout), so a park only reaches this bound when
// the database is stalled; it then parks exactly as it did before the
// rendezvous existed, and the audit and the backstop bound the wake.
const narrowedParkRendezvousTimeout = 3 * time.Second

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
// shared advisory lock on every key, true when none was held exclusively. It
// attempts every key (the aggregate consumes every row), and as an autocommit
// statement releases whatever it took when it returns. $1 is the text[] of
// activationRendezvousKey values.
const tryBlockingActivationsQuery = `
SELECT COALESCE(bool_and(pg_try_advisory_xact_lock_shared(hashtextextended(k, 0))), TRUE)
FROM unnest($1::text[]) AS k`

// awaitBlockingActivationsQuery is the park side, contended path: a shared
// advisory lock on every key that waits behind an in-flight activation and
// returns as soon as that activation commits or rolls back. It holds no other
// lock while it waits, so it cannot join a lock cycle with an activator (which
// takes one scope's lock first and nothing of the parker's). $1 is the text[]
// of activationRendezvousKey values.
const awaitBlockingActivationsQuery = `
SELECT count(*) FROM (
    SELECT pg_advisory_xact_lock_shared(hashtextextended(k, 0)) FROM unnest($1::text[]) AS k
) AS held`

// rendezvousScopeIDs lists the scopes whose in-flight activation a parking
// wait must stand behind: the blocking set of a narrowed wait, or the waiter's
// own scope for a coarse generation-activation wait (its provider is the
// activation of its own generation). Other wait kinds have other providers
// (a producer's acknowledgement, a phase publish, a winners rebuild) with their
// own transactions and lock orders, and take no rendezvous here.
func rendezvousScopeIDs(kind WaitingWakeKind, key waitingKey) []string {
	if len(key.BlockingScopeIDs) > 0 {
		return key.BlockingScopeIDs
	}
	if kind == WaitingKindRelationshipGenerationActive && key.ScopeID != "" {
		return []string{key.ScopeID}
	}
	return nil
}

// awaitInFlightActivations closes the park-versus-activation window of a
// generation-activation wait (#11256). A wait is inserted and then checked for
// satisfaction. An activation commits its generation and runs its wake in one
// transaction. Without a rendezvous, an activation whose wake statement ran
// before the wait row committed misses the row (it cannot see it), and the
// parker's check, which runs before that activation commits, misses the
// activation (it cannot see it either), so the wait sleeps to the audit or the
// backstop. Taking the shared lock between the insert and the check orders the
// two: a parker whose insert committed after the activation began waits here
// for its commit and then checks against the committed activation, and a
// parker that passes the lock before any activation began inserted its row
// before that activation's wake snapshot, so the wake sees it.
//
// Only the kinds rendezvousScopeIDs names take the rendezvous. A park that
// finds no activation in flight costs one statement and records nothing. One
// that has to wait records outcome waited on the park_rendezvous site. A
// timeout or cancellation of the wait degrades to the prior behaviour (the
// row stays parked, the audit and the backstop bound it) and records outcome
// timeout. Any other error is returned so the Fail path retries.
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
	var free bool
	if err := querySingleRow(waitCtx, database, tryBlockingActivationsQuery,
		[]any{array.StringArray(keys)}, &free); err != nil {
		return rendezvousError(ctx, waitCtx, instruments, scopeIDs, err)
	}
	if free {
		return nil
	}
	var held int64
	if err := querySingleRow(waitCtx, database, awaitBlockingActivationsQuery,
		[]any{array.StringArray(keys)}, &held); err != nil {
		return rendezvousError(ctx, waitCtx, instruments, scopeIDs, err)
	}
	recordWaitingWake(ctx, instruments, telemetry.WaitingWakeSiteParkRendezvous, telemetry.WaitingWakeOutcomeWaited)
	return nil
}

// rendezvousError turns a failed rendezvous statement into the park's
// outcome: the bound firing (the caller's context still live) is a timeout
// the park survives; anything else is returned.
func rendezvousError(
	ctx, waitCtx context.Context,
	instruments *telemetry.Instruments,
	scopeIDs []string,
	err error,
) error {
	if waitCtx.Err() != nil && ctx.Err() == nil {
		recordWaitingWake(ctx, instruments, telemetry.WaitingWakeSiteParkRendezvous, telemetry.WaitingWakeOutcomeTimeout)
		slog.Default().WarnContext(ctx, "waiting park stopped waiting for an in-flight activation; the audit bounds the wake",
			"event_name", "reducer_waiting.narrowed_park_rendezvous_timeout",
			"provider_scope_ids", scopeIDs,
			"timeout", narrowedParkRendezvousTimeout.String())
		return nil
	}
	return fmt.Errorf("await in-flight activations: %w", err)
}

// narrowedCorpusWaitSatisfiedQuery reports whether one narrowed corpus wait
// may proceed: the waiter's own relationship generation is active AND no
// scope in the verdict-time blocking set still holds the fence. $1 scope,
// $2 generation, $3 blocking scope IDs (text[]).
const narrowedCorpusWaitSatisfiedQuery = `
SELECT EXISTS (
    SELECT 1 FROM relationship_generations AS rg
    WHERE rg.scope = $1 AND rg.generation_id = $2 AND rg.status = 'active'
) AND NOT EXISTS (
    SELECT 1 FROM ingestion_scopes AS s
    WHERE s.scope_id = ANY ($3::text[])
      AND` + incompleteScopeRelationshipGenerationsPredicate + `
)`

// waitingSatisfied reports whether the provider of one wait already
// committed, sharing the single predicate the audit uses. A narrowed corpus
// wait (blocking set present) uses the narrowed predicate: own generation
// active and the verdict-time blocking set clear. A phase-exact wait (#11104)
// uses the exact phase-row predicate, never the coarse any-phase branch,
// which would satisfy at park time and spin.
func waitingSatisfied(
	ctx context.Context,
	database db.ExecQueryer,
	kind WaitingWakeKind,
	scopeID, generationID, producerDomain, enqueuedAt, waiterID string,
	fenceDomains []string,
	blockingScopeIDs []string,
	phase waitingPhaseRequirement,
	progressSince time.Time,
) (bool, error) {
	if kind == WaitingKindPhaseCommitted && phase.exact() {
		return exactPhaseWaitSatisfied(ctx, database, scopeID, generationID, phase)
	}
	if kind == WaitingKindRelationshipGenerationActive && len(blockingScopeIDs) > 0 {
		var satisfied bool
		if err := querySingleRow(ctx, database, narrowedCorpusWaitSatisfiedQuery, []any{
			scopeID, generationID, array.StringArray(blockingScopeIDs),
		}, &satisfied); err != nil {
			return false, fmt.Errorf("check narrowed waiting satisfaction: %w", err)
		}
		return satisfied, nil
	}
	// The domain-only producer branch evaluates the handler quiescence
	// rule on the parked domain's mapped collector kind (empty when the
	// domain maps to none, which counts as satisfied).
	collectorKinds := []string{}
	if kind == WaitingKindProducerTerminal && producerDomain != "" && scopeID == "" {
		if collectorKind, ok := crossscope.CollectorKindForProducerDomain(contract.Domain(producerDomain)); ok {
			collectorKinds = []string{collectorKind}
		}
	}
	var satisfied bool
	if err := querySingleRow(ctx, database, waitingSatisfiedQuery, []any{
		string(kind), scopeID, generationID, producerDomain,
		enqueuedAt, array.StringArray(fenceDomains),
		waiterID, progressSince.UTC(),
		array.StringArray(collectorKinds),
	}, &satisfied); err != nil {
		return false, fmt.Errorf("check waiting satisfaction: %w", err)
	}
	return satisfied, nil
}

// querySingleRow runs a query that returns exactly one row with one column
// group and scans it, following the store's QueryContext-only adapter
// surface (db.ExecQueryer has no QueryRowContext).
func querySingleRow(ctx context.Context, database db.ExecQueryer, query string, args []any, dest ...any) error {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

// waitingSatisfiedForIntent evaluates the shared satisfaction predicate for
// one intent's wait key, narrowed when the key carries a blocking set.
func waitingSatisfiedForIntent(
	ctx context.Context,
	database db.ExecQueryer,
	intent contract.Intent,
	entry WaitingDeferral,
	keyJSON string,
	now time.Time,
) (bool, error) {
	var key waitingKey
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		return false, fmt.Errorf("decode waiting key: %w", err)
	}
	scopeID := strings.TrimSpace(intent.ScopeID)
	generationID := strings.TrimSpace(intent.GenerationID)
	producerDomain := entry.ProducerDomain
	if entry.WaitsForKind == WaitingKindProducerTerminal && key.ScopeID == "" && key.ProducerDomain != "" {
		// #11000: a domain-only wait names no scope or generation, so the
		// legacy scope-pinned branch can never match; the audit evaluates
		// the quiescence branch on the parked domain instead.
		scopeID, generationID, producerDomain = "", "", key.ProducerDomain
	}
	return waitingSatisfied(
		ctx, database, entry.WaitsForKind,
		scopeID, generationID,
		producerDomain, key.EnqueuedAt, intent.IntentID,
		key.Domains, key.BlockingScopeIDs,
		waitingPhaseRequirement{Keyspace: key.Keyspace, Phase: key.Phase}, now,
	)
}

// wakeNarrowedCorpusWaitsForScopeQuery reopens narrowed corpus waits naming
// the activated scope in their blocking set. The retried handler re-checks
// the restricted fence, so over-wake (a sibling blocker still open) only
// costs one re-deferral, while the audit and the backstop bound a missed one.
// $1 is the wake timestamp, $2 the activated scope. Waiting rows lock in
// work_item_id COLLATE "C" order, the same global order every multi-row
// reducer locker uses, so a concurrent wake cannot deadlock it.
const wakeNarrowedCorpusWaitsForScopeQuery = `
WITH due AS (
    SELECT work_item_id, waits_for_key FROM reducer_waiting_items
    WHERE waits_for_kind = 'relationship_generation_active'
      AND waits_for_key->'blocking_scope_ids' @> to_jsonb($2::text)
    ORDER BY work_item_id COLLATE "C"
    FOR UPDATE
),
reopened AS (
    UPDATE fact_work_items AS work
    SET ` + waitingWakeReopenSetClause + `
    FROM due
    WHERE work.work_item_id = due.work_item_id
      AND work.stage = 'reducer'
      AND work.status = 'retrying'
    RETURNING work.work_item_id, due.waits_for_key
),
cleared AS (
    DELETE FROM reducer_waiting_items AS wait
    USING reopened
    WHERE wait.work_item_id = reopened.work_item_id
      AND wait.waits_for_kind = 'relationship_generation_active'
      AND wait.waits_for_key = reopened.waits_for_key
    RETURNING 1
)
SELECT (SELECT count(*) FROM reopened), (SELECT count(*) FROM cleared)
`

// wakeNarrowedCorpusWaitsForScope reopens every narrowed corpus wait blocked
// on the activated scope. It runs inside the activation transaction, after
// the coarse own-generation wake.
func wakeNarrowedCorpusWaitsForScope(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	scopeID string,
	now time.Time,
) error {
	var reopenedCount, clearedCount int64
	if err := querySingleRow(ctx, database, wakeNarrowedCorpusWaitsForScopeQuery,
		[]any{now, scopeID}, &reopenedCount, &clearedCount); err != nil {
		recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("wake narrowed corpus waiting items: %w", err)
	}
	outcome := telemetry.WaitingWakeOutcomeNoop
	if reopenedCount > 0 {
		outcome = telemetry.WaitingWakeOutcomeReopened
	}
	recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, outcome)
	return nil
}
