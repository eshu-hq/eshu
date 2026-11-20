// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WaitingBackstopDelay is the visible_at horizon of a parked wait row
// (#10876 decision 4): even if the wake and the audit both miss, the row
// becomes claimable this long after the deferral. Ten minutes at the
// production 5-minute audit interval bounds detection at two cycles.
const WaitingBackstopDelay = 10 * time.Minute

// DefaultWaitingAuditInterval is the slow-audit period (#10876 decision 4):
// a periodic re-evaluation that reopens waits whose provider already
// committed. Five minutes with the 10-minute backstop gives the 10-minute
// visible_at bound the acceptance criterion requires.
const DefaultWaitingAuditInterval = 5 * time.Minute

// DefaultWaitingAuditBatchSize bounds one audit pass over the waiting table.
const DefaultWaitingAuditBatchSize = 500

// waitingKey is the JSON waits_for_key per kind. Scope/generation keys stay
// coarse on purpose: the wake is a hint and the claim gate plus the handler
// re-check the exact requirement, so a wake never starts work early.
type waitingKey struct {
	ScopeID        string   `json:"scope_id,omitempty"`
	GenerationID   string   `json:"generation_id,omitempty"`
	ProducerDomain string   `json:"producer_domain,omitempty"`
	EnqueuedAt     string   `json:"enqueued_at,omitempty"`
	Domains        []string `json:"domains,omitempty"`
}

// waitingParkDelay resolves the retry horizon for a deferred intent: the
// 10-minute backstop when the failure class parks in the waiting set-aside,
// else the exponential backoff (#10876). A parked readiness wait keeps its
// queue row in the same non-counting class; the wait row carries the
// waits-for. Counting and not-a-wait classes keep the backoff path
// unchanged. ok is false when the class never parks, and then entry is zero.
func waitingParkDelay(retryFailureClass string, backoff time.Duration) (time.Duration, WaitingDeferral, bool) {
	entry, ok := WaitingDeferralByClass()[retryFailureClass]
	if !ok || entry.WakeSite == WaitingWakeSiteCounting || entry.WakeSite == WaitingWakeSiteNotAWait {
		return backoff, WaitingDeferral{}, false
	}
	return WaitingBackstopDelay, entry, true
}

// waitingKeyFor derives the waits_for key for one parked wait from the
// intent. It reports false when the kind needs identity the intent does not
// carry; the caller then keeps the existing timer path instead of parking a
// wait that can never wake.
func waitingKeyFor(intent reducercontract.Intent, entry WaitingDeferral) (string, bool) {
	scope := strings.TrimSpace(intent.ScopeID)
	gen := strings.TrimSpace(intent.GenerationID)
	if scope == "" || gen == "" {
		return "", false
	}
	var key waitingKey
	switch entry.WaitsForKind {
	case WaitingKindRelationshipGenerationActive,
		WaitingKindPhaseCommitted,
		WaitingKindGenerationActive,
		WaitingKindTargetCommitted:
		key = waitingKey{ScopeID: scope, GenerationID: gen}
	case WaitingKindProducerTerminal:
		if entry.ProducerDomain == "" {
			key = waitingKey{ScopeID: scope, GenerationID: gen}
			break
		}
		key = waitingKey{ScopeID: scope, GenerationID: gen, ProducerDomain: entry.ProducerDomain}
	case WaitingKindWinnersWatermark:
		enqueueAt := intent.CycleStartedAt
		if enqueueAt.IsZero() {
			enqueueAt = intent.EnqueuedAt
		}
		if enqueueAt.IsZero() {
			return "", false
		}
		key = waitingKey{EnqueuedAt: enqueueAt.UTC().Format(time.RFC3339Nano)}
	case WaitingKindFenceClear:
		key = waitingKey{ScopeID: scope, GenerationID: gen, Domains: waitingFenceDomains(entry.FailureClass)}
	default:
		return "", false
	}
	raw, err := json.Marshal(key)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// waitingFenceDomains names the queue domains whose nonterminal rows hold
// the fence open, mirroring the handler's own fence predicate so the audit
// re-checks the same condition the handler refused on.
func waitingFenceDomains(failureClass string) []string {
	if failureClass == reducer.CloudAdmissionNotReadyFailureClass {
		return []string{string(reducer.DomainCloudInventoryAdmission)}
	}
	out := make([]string, 0, len(ValueFlowInputsFenceReducerDomains))
	for _, domain := range ValueFlowInputsFenceReducerDomains {
		out = append(out, string(domain))
	}
	return out
}

// insertWaitingItemQuery parks one wait row. The primary-key upsert keeps a
// repeated Fail for the same wait idempotent: the first deferral wins as the
// anchor and later ones only refresh updated_at.
const insertWaitingItemQuery = `
INSERT INTO reducer_waiting_items (
    work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at
) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $8)
ON CONFLICT (work_item_id, waits_for_kind, waits_for_key) DO UPDATE SET
    updated_at = EXCLUDED.updated_at
`

// waitingSatisfiedQuery reports whether the provider of one wait key already
// committed. One branch per SQL-provable kind (#10876 decision 4): the audit
// and the set-aside re-check share it, so a waiter that registers after its
// provider committed never strands.
// $1 kind, $2 scope_id, $3 generation_id, $4 producer_domain,
// $5 enqueued_at (RFC3339Nano, winners only), $6 fence domains (text[]),
// $7 waiter work_item_id (excluded from the fence's own-row check),
// $8 progress-since timestamp (target fallback: terminal progress after the
// deferral deserves one re-check).
const waitingSatisfiedQuery = `
SELECT EXISTS (
    SELECT 1 FROM scope_generations AS gen
    WHERE $1 = 'generation_active'
      AND gen.scope_id = $2 AND gen.generation_id = $3 AND gen.status = 'active'
) OR EXISTS (
    SELECT 1 FROM relationship_generations AS rg
    WHERE $1 = 'relationship_generation_active'
      AND rg.scope = $2 AND rg.generation_id = $3 AND rg.status = 'active'
) OR EXISTS (
    SELECT 1 FROM graph_projection_phase_state AS phase
    WHERE $1 = 'phase_committed'
      AND phase.scope_id = $2 AND phase.generation_id = $3
) OR EXISTS (
    SELECT 1 FROM fact_work_items AS producer
    WHERE $1 = 'producer_domain_terminal' AND $4 <> ''
      AND producer.scope_id = $2 AND producer.generation_id = $3
      AND producer.stage = 'reducer' AND producer.domain = $4
      AND producer.status IN ('succeeded', 'failed', 'dead_letter', 'superseded')
) OR (
    $1 = 'winners_watermark' AND $5 <> '' AND EXISTS (
        SELECT 1 FROM supply_chain_impact_winners_materialization AS wm
        WHERE wm.singleton = 1
          -- >=, not >: the handler proceeds when the watermark reaches the
          -- bound (!watermark.Before(required)), and the in-tx wake aligns
          -- the woken row's cycle start to the covering stamp, so the
          -- post-wake claim presents bound == watermark. A strict > would
          -- report that claim unsatisfied while the handler accepts it.
          AND wm.materialized_at >= ($5::timestamptz)
    )
) OR (
    $1 = 'fence_clear' AND NOT EXISTS (
        SELECT 1 FROM fact_work_items AS fenced
        WHERE fenced.scope_id = $2 AND fenced.generation_id = $3
          AND fenced.stage = 'reducer'
          AND fenced.domain = ANY ($6::text[])
          AND fenced.status IN ('pending', 'retrying', 'claimed', 'running')
          AND fenced.work_item_id <> $7
    )
) OR EXISTS (
    SELECT 1 FROM fact_work_items AS progress
    WHERE $1 = 'target_committed'
      AND progress.scope_id = $2 AND progress.generation_id = $3
      AND progress.stage = 'reducer' AND progress.status = 'succeeded'
      AND progress.updated_at > $8::timestamptz
)
`

// waitingSatisfied reports whether the provider of one wait already
// committed, sharing the single predicate the audit uses.
func waitingSatisfied(
	ctx context.Context,
	database db.ExecQueryer,
	kind WaitingWakeKind,
	scopeID, generationID, producerDomain, enqueuedAt, waiterID string,
	fenceDomains []string,
	progressSince time.Time,
) (bool, error) {
	var satisfied bool
	if err := querySingleRow(ctx, database, waitingSatisfiedQuery, []any{
		string(kind), scopeID, generationID, producerDomain,
		enqueuedAt, "{" + strings.Join(quoteSQLStrings(fenceDomains), ",") + "}",
		waiterID, progressSince.UTC(),
	}, &satisfied); err != nil {
		return false, fmt.Errorf("check waiting satisfaction: %w", err)
	}
	return satisfied, nil
}

// quoteSQLStrings renders values as a Postgres text[] literal body.
func quoteSQLStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, `"`+strings.ReplaceAll(value, `"`, `""`)+`"`)
	}
	return out
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

// waitingWakeReopenSetClause is the SET list of the #10876 waiting wake. It
// deliberately differs from activationWakeSetClause in one load-bearing way:
// the row stays 'retrying' in its non-counting class instead of moving to
// 'pending'. A pending row's next claim increments attempt_count (the claim
// CASE only freezes retrying rows in a non-counting class), so a
// pending-wake burns one attempt per park cycle and an elapsed-bounded wait
// dead-letters after maxAttempts parks — the exact budget erosion the
// set-aside exists to prevent (proven by the both-orders live test: pending
// ends at attempt 2, retrying at attempt 1). A retrying row with a past
// visible_at is claimable at once, and Ack clears the class on success while
// a re-Fail overwrites it, so no stale class survives an outcome. $1 is the
// wake timestamp.
const waitingWakeReopenSetClause = `status = 'retrying',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        updated_at = $1,
        reopened_at = $1`

// reopenWaitingRowsQuery reopens parked queue rows for one wait key and
// deletes their wait rows. $1 is the wake timestamp, $2 the kind, $3 the
// key. Waiting rows lock in work_item_id COLLATE "C" order, the same global
// order every multi-row reducer locker uses, so a concurrent wake cannot
// deadlock it; the provider row is already held by the caller and the queue
// rows take no lock the claim's SKIP LOCKED cannot skip.
const reopenWaitingRowsQuery = `
WITH due AS (
    SELECT work_item_id FROM reducer_waiting_items
    WHERE waits_for_kind = $2 AND waits_for_key = $3::jsonb
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
    RETURNING work.work_item_id
),
cleared AS (
    DELETE FROM reducer_waiting_items AS wait
    USING reopened
    WHERE wait.work_item_id = reopened.work_item_id
      AND wait.waits_for_kind = $2
      AND wait.waits_for_key = $3::jsonb
    RETURNING 1
)
SELECT (SELECT count(*) FROM reopened), (SELECT count(*) FROM cleared)
`

// wakeWaitingByKey reopens every wait parked on one waits_for key.
// site names the wake site for telemetry (in_tx, audit). It returns reopened
// and cleared counts.
func wakeWaitingByKey(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	kind WaitingWakeKind,
	keyJSON string,
	now time.Time,
) (reopened, cleared int64, err error) {
	var reopenedCount, clearedCount int64
	if err := querySingleRow(ctx, database, reopenWaitingRowsQuery,
		[]any{now, string(kind), keyJSON}, &reopenedCount, &clearedCount); err != nil {
		recordWaitingWake(ctx, instruments, site, telemetry.WaitingWakeOutcomeError)
		return 0, 0, fmt.Errorf("wake waiting items: %w", err)
	}
	outcome := telemetry.WaitingWakeOutcomeNoop
	if reopenedCount > 0 {
		outcome = telemetry.WaitingWakeOutcomeReopened
	}
	recordWaitingWake(ctx, instruments, site, outcome)
	return reopenedCount, clearedCount, nil
}

// setAsideWaiting parks one deferred intent: it inserts the wait row and,
// when the provider already committed, reopens it at once so a waiter that
// registers after its provider never strands (the same lost-wake pattern as
// the #7597 Fail-path wake). It returns true when the wait is parked (or
// reopened at once) and false when no wait row applies.
func setAsideWaiting(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intent reducercontract.Intent,
	entry WaitingDeferral,
	now time.Time,
) (bool, error) {
	if entry.WakeSite == WaitingWakeSiteCounting || entry.WakeSite == WaitingWakeSiteNotAWait {
		return false, nil
	}
	keyJSON, ok := waitingKeyFor(intent, entry)
	if !ok {
		return false, nil
	}
	if _, err := database.ExecContext(ctx, insertWaitingItemQuery,
		intent.IntentID, string(entry.WaitsForKind), keyJSON,
		strings.TrimSpace(intent.ScopeID), strings.TrimSpace(intent.GenerationID),
		string(intent.Domain), entry.FailureClass, now,
	); err != nil {
		return false, fmt.Errorf("park waiting reducer work: %w", err)
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

// waitingSatisfiedForIntent evaluates the shared satisfaction predicate for
// one intent's wait key.
func waitingSatisfiedForIntent(
	ctx context.Context,
	database db.ExecQueryer,
	intent reducercontract.Intent,
	entry WaitingDeferral,
	keyJSON string,
	now time.Time,
) (bool, error) {
	var key waitingKey
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		return false, fmt.Errorf("decode waiting key: %w", err)
	}
	return waitingSatisfied(
		ctx, database, entry.WaitsForKind,
		strings.TrimSpace(intent.ScopeID), strings.TrimSpace(intent.GenerationID),
		entry.ProducerDomain, key.EnqueuedAt, intent.IntentID,
		key.Domains, now,
	)
}

// waitingProducerDomains is the set of producer domains the deferral map
// narrows a producer-terminal wait to. A terminal Ack of one of these
// domains reopens exactly the waits that named it — no fleet fanout.
func waitingProducerDomains() map[string]bool {
	out := map[string]bool{}
	for _, entry := range WaitingDeferralMap() {
		if entry.WaitsForKind == WaitingKindProducerTerminal && entry.ProducerDomain != "" {
			out[entry.ProducerDomain] = true
		}
	}
	return out
}

// wakeProducerTerminalAfterAck reopens producer-terminal waits after terminal
// reducer ACKs. It runs after the ACK committed (Ack is a single statement,
// not a transaction), keyed narrowly to the acked (scope, generation,
// producer domain), so it never fans out. A waiter that parks after this Ack
// is covered by the set-aside re-check; one that parked before and whose
// provider committed without an Ack is covered by the audit fallback.
func wakeProducerTerminalAfterAck(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intents []reducercontract.Intent,
	now time.Time,
) error {
	producers := waitingProducerDomains()
	seen := map[string]struct{}{}
	for _, intent := range intents {
		domain := string(intent.Domain)
		if !producers[domain] {
			continue
		}
		scope := strings.TrimSpace(intent.ScopeID)
		gen := strings.TrimSpace(intent.GenerationID)
		if scope == "" || gen == "" {
			continue
		}
		keyJSON, err := json.Marshal(waitingKey{ScopeID: scope, GenerationID: gen, ProducerDomain: domain})
		if err != nil {
			return fmt.Errorf("encode producer-terminal wake key: %w", err)
		}
		key := string(keyJSON)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, WaitingWakeSiteInTx,
			WaitingKindProducerTerminal, key, now); err != nil {
			return err
		}
	}
	return nil
}
