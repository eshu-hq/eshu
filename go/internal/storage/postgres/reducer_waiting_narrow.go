// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// waitingKey is the JSON waits_for_key per kind. BlockingScopeIDs is set only
// on narrowed corpus waits (a bounded sorted prefix of the verdict set, see
// narrowedWaitingKey); coarse waits omit it. RepoIDs is decode-only: rows
// parked before #11256 carry the read set there, and nothing reads it, so new
// keys never write it.
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
	CorpusWaitBlockingScopeIDs() []string
}

// Narrowed wait key bounds (#11256). waits_for_key sits in two btree indexes
// (the primary key and the wake index, migration 316) whose entries may not
// exceed 2704 bytes after compression, and the verdict-time blocking set is
// unbounded (a bootstrap can leave every incomplete git scope blocking). The
// key therefore names a bounded sorted prefix of the set. That is exact: the
// activation wake is a hint and the retried handler re-checks the whole
// restricted fence, and once at most narrowedWaitKeyMaxBlockingScopes blockers
// remain every one is named, so the last activation always wakes the item.
const (
	// narrowedWaitKeyMaxBlockingScopes caps the blocking scopes a key names.
	narrowedWaitKeyMaxBlockingScopes = 16
	// narrowedWaitKeyMaxBytes caps the rendered key, scope_id and generation_id
	// included.
	narrowedWaitKeyMaxBytes = 1024
	// waitingRowIndexBudgetBytes caps the uncompressed size of a wait row's
	// indexed columns (work_item_id, waits_for_kind and the rendered key). A
	// narrowed key is bounded above, but work_item_id, scope_id and
	// generation_id have no length bound and the other wait kinds carry them
	// in their keys. Measured on postgres 18.6 with incompressible text, both
	// indexes accept rows up to about 2.6 KB of composite text for any split
	// between work_item_id and key; the budget leaves the rest for the jsonb
	// encoding and the index tuple header.
	waitingRowIndexBudgetBytes = 2048
)

// narrowedWaitKey is a rendered narrowed wait key with how much of the
// verdict set it names. JSON is empty when no blocking scope fits the key.
type narrowedWaitKey struct {
	JSON  string
	Named int
	Total int
}

// narrowedWaitingKey renders the narrowed corpus waits_for key for one parked
// intent: {scope_id, generation_id, blocking_scope_ids}, where
// blocking_scope_ids is the longest sorted prefix of the verdict set that fits
// narrowedWaitKeyMaxBlockingScopes IDs and narrowedWaitKeyMaxBytes of rendered
// key. An ID that cannot fit even alone is skipped. The set is normalized
// (trimmed, de-duplicated, sorted) first, so one set in any order renders one
// key and a repeated park stays one idempotent row. It reports false when the
// waiter has no scope or generation or the verdict set is empty (the caller
// then parks the coarse wait), and a key with no JSON when a set is present
// but no ID fits.
func narrowedWaitingKey(scopeID, generationID string, blockingScopeIDs []string) (narrowedWaitKey, bool) {
	scope := strings.TrimSpace(scopeID)
	gen := strings.TrimSpace(generationID)
	if scope == "" || gen == "" {
		return narrowedWaitKey{}, false
	}
	blocking := normalizeWaitingScopeIDs(blockingScopeIDs)
	if len(blocking) == 0 {
		return narrowedWaitKey{}, false
	}
	out := narrowedWaitKey{Total: len(blocking)}
	base, err := json.Marshal(waitingKey{ScopeID: scope, GenerationID: gen})
	if err != nil {
		return out, true
	}
	baseSize := len(base) + len(`,"blocking_scope_ids":[]`)
	size := baseSize
	chosen := make([]string, 0, narrowedWaitKeyMaxBlockingScopes)
	for _, id := range blocking {
		if len(chosen) == narrowedWaitKeyMaxBlockingScopes {
			break
		}
		quoted, err := json.Marshal(id)
		if err != nil || baseSize+len(quoted) > narrowedWaitKeyMaxBytes {
			continue // cannot fit alone
		}
		separator := 0
		if len(chosen) > 0 {
			separator = 1
		}
		if size+separator+len(quoted) > narrowedWaitKeyMaxBytes {
			break // the sorted prefix ends here
		}
		chosen = append(chosen, id)
		size += separator + len(quoted)
	}
	if len(chosen) == 0 {
		return out, true
	}
	raw, err := json.Marshal(waitingKey{ScopeID: scope, GenerationID: gen, BlockingScopeIDs: chosen})
	if err != nil || len(raw) > narrowedWaitKeyMaxBytes {
		return out, true
	}
	out.JSON, out.Named = string(raw), len(chosen)
	return out, true
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
// cause that carries the verdict-time blocking set, for the generation-active
// kind only. The second result is false when the cause is not a narrowing
// deferral (the caller parks the coarse wait); a true result with an empty
// JSON means the set has no ID that fits the key.
func narrowedWaitingKeyForCause(
	intent contract.Intent,
	entry WaitingDeferral,
	cause error,
) (narrowedWaitKey, bool) {
	if entry.WaitsForKind != WaitingKindRelationshipGenerationActive {
		return narrowedWaitKey{}, false
	}
	var narrowing corpusWaitNarrowing
	if cause == nil || !errors.As(cause, &narrowing) {
		return narrowedWaitKey{}, false
	}
	return narrowedWaitingKey(intent.ScopeID, intent.GenerationID, narrowing.CorpusWaitBlockingScopeIDs())
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
	if key, ok := narrowedWaitingKeyForCause(intent, entry, cause); ok {
		if key.JSON == "" {
			// A narrowing deferral whose blocking set has no ID that fits the
			// key parks nothing. The coarse wait is no substitute: it is
			// satisfied by the waiter's own generation being active, so it
			// would reopen the item at once and spin. The queue row keeps the
			// backstop timer the retry UPDATE already set.
			recordWaitingKeyOversize(ctx, instruments, intent, entry.WaitsForKind, 0, key.Total)
			return false, nil
		}
		parked, err := setAsideWaitingForKey(ctx, database, instruments, intent, entry, key.JSON, now)
		if err == nil && parked && key.Named < key.Total {
			recordWaitingKeyTruncated(ctx, instruments, intent)
		}
		return parked, err
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
	inserted, err := insertWaitingItem(ctx, database, instruments, intent, entry, keyJSON, now)
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, nil
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
