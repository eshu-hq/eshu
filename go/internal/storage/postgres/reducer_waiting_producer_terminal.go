// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// waitingProducerDomainKey renders the #11000 waits_for_key for one parked
// producer domain: the domain alone, with no scope or generation. A
// cross-scope producer commits in its own scopes, so any scope-pinned key
// can never match; the domain-only key matches every commit of that
// producer. An empty result means the key is unusable and the caller parks
// nothing.
func waitingProducerDomainKey(producerDomain string) string {
	domain := strings.TrimSpace(producerDomain)
	if domain == "" {
		return ""
	}
	raw, err := json.Marshal(waitingKey{ProducerDomain: domain})
	if err != nil {
		return ""
	}
	return string(raw)
}

// producerDomainsForCollectorKind resolves an acked projector scope's
// collector kind to the producer domains it publishes, through the same
// crossscope map the handler floor reads. The projector-Ack wake fans out
// to exactly these domain keys.
func producerDomainsForCollectorKind(collectorKind string) []reducercontract.Domain {
	kind := strings.TrimSpace(collectorKind)
	if kind == "" {
		return nil
	}
	var out []reducercontract.Domain
	for producer := range completionEdgeProducerSet() {
		if mapped, ok := crossscope.CollectorKindForProducerDomain(producer); ok && mapped == kind {
			out = append(out, producer)
		}
	}
	slices.Sort(out)
	return out
}

// crossScopeParkGate resolves the #11000 park/no-park decision for the
// cross-scope producer class: the floor error's own unready set, else the
// catalog derivation for the consumer. When neither yields a domain the
// wait could never wake, so the intent keeps the existing timer path
// (backoff) instead of parking.
func crossScopeParkGate(
	retryFailureClass string,
	cause error,
	consumer reducercontract.Domain,
	parkWait bool,
	backoff, delay time.Duration,
) ([]reducercontract.Domain, bool, time.Duration) {
	if !parkWait || retryFailureClass != crossscope.ProducerNotReadyFailureClass {
		return nil, parkWait, delay
	}
	if domains := producerTerminalParkDomains(cause, consumer); len(domains) > 0 {
		return domains, true, delay
	}
	return nil, false, backoff
}

// parkProducerTerminalWaits parks one domain-only wait per producer domain
// and re-checks each at once, so a waiter that registers after its provider
// committed never strands. It returns true when at least one wait row
// applies.
func parkProducerTerminalWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intent reducercontract.Intent,
	entry WaitingDeferral,
	producerDomains []reducercontract.Domain,
	now time.Time,
) (bool, error) {
	seen := map[string]bool{}
	parked := false
	for _, producer := range producerDomains {
		keyJSON := waitingProducerDomainKey(string(producer))
		if keyJSON == "" || seen[keyJSON] {
			continue
		}
		seen[keyJSON] = true
		if err := insertWaitingItem(ctx, database, intent, entry, keyJSON, now); err != nil {
			return parked, err
		}
		parked = true
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
	}
	return parked, nil
}

// insertWaitingItem parks one wait row. The primary-key upsert keeps a
// repeated Fail for the same wait idempotent: the first deferral wins as
// the anchor and later ones only refresh updated_at.
func insertWaitingItem(
	ctx context.Context,
	database db.ExecQueryer,
	intent reducercontract.Intent,
	entry WaitingDeferral,
	keyJSON string,
	now time.Time,
) error {
	if _, err := database.ExecContext(ctx, insertWaitingItemQuery,
		intent.IntentID, string(entry.WaitsForKind), keyJSON,
		strings.TrimSpace(intent.ScopeID), strings.TrimSpace(intent.GenerationID),
		string(intent.Domain), entry.FailureClass, now,
	); err != nil {
		return fmt.Errorf("park waiting reducer work: %w", err)
	}
	return nil
}

// waitingProducerDomains is the set of producer domains the deferral map
// narrows a same-scope producer-terminal wait to. A terminal Ack of one of
// these domains reopens exactly the waits that named it — no fleet fanout.
func waitingProducerDomains() map[string]bool {
	out := map[string]bool{}
	for _, entry := range WaitingDeferralMap() {
		if entry.WaitsForKind == WaitingKindProducerTerminal && entry.ProducerDomain != "" {
			out[entry.ProducerDomain] = true
		}
	}
	return out
}

// wakeProducerTerminalCrossScopeQuery reopens scope-bound producer-terminal
// waits naming one producer domain, in any scope, and deletes their wait
// rows. $1 is the wake timestamp, $2 the producer domain. A cross-scope
// waiter (a catalog correlation parked on a repository-scope workload, a
// pattern the #8827/#7816 contention proofs pin) commits its provider in a
// scope the narrowed same-scope wake never names and the domain-only wake
// never matches, so without this leg it sleeps to the 10-minute backstop.
// The predicate mirrors reopenWaitingRowsQuery row for row except for the
// key match: same reopen set clause, same retrying-only reopen, same
// work_item_id COLLATE "C" lock order. The scan is bounded by the parked
// set, not the fleet: only deferred waits have rows here.
const wakeProducerTerminalCrossScopeQuery = `
WITH due AS (
    SELECT work_item_id FROM reducer_waiting_items
    WHERE waits_for_kind = 'producer_domain_terminal'
      AND waits_for_key->>'producer_domain' = $2
      AND waits_for_key ? 'scope_id'
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
      AND wait.waits_for_kind = 'producer_domain_terminal'
      AND wait.waits_for_key->>'producer_domain' = $2
      AND wait.waits_for_key ? 'scope_id'
    RETURNING 1
)
SELECT (SELECT count(*) FROM reopened), (SELECT count(*) FROM cleared)
`

// wakeProducerTerminalCrossScope reopens every scope-bound wait parked on
// one producer domain. Site and telemetry mirror wakeWaitingByKey.
func wakeProducerTerminalCrossScope(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	domain string,
	now time.Time,
) error {
	var reopenedCount, clearedCount int64
	if err := querySingleRow(ctx, database, wakeProducerTerminalCrossScopeQuery,
		[]any{now, domain}, &reopenedCount, &clearedCount); err != nil {
		recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("wake cross-scope producer-terminal waits: %w", err)
	}
	outcome := telemetry.WaitingWakeOutcomeNoop
	if reopenedCount > 0 {
		outcome = telemetry.WaitingWakeOutcomeReopened
	}
	recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, outcome)
	return nil
}

// wakeProducerTerminalAfterAck reopens producer-terminal waits after terminal
// reducer ACKs. It runs after the ACK committed (Ack is a single statement,
// not a transaction; the synchronous post-commit wake is the established
// #10876 pattern for the reducer ACKs). Narrowed same-scope waits wake
// keyed narrowly to the acked (scope, generation, producer domain), so they
// never fan out. Cross-scope waits wake keyed to the acked producer domain
// alone when that domain is a CompletionEdges producer, regardless of the
// acked scope. A waiter that parks after this Ack is covered by the
// set-aside re-check; one that parked before and whose provider committed
// without an Ack is covered by the audit fallback.
func wakeProducerTerminalAfterAck(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intents []reducercontract.Intent,
	now time.Time,
) error {
	narrowed := waitingProducerDomains()
	producers := completionEdgeProducerSet()
	seen := map[string]struct{}{}
	crossScoped := map[string]struct{}{}
	wake := func(key string) error {
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, WaitingWakeSiteInTx,
			WaitingKindProducerTerminal, key, now); err != nil {
			return err
		}
		return nil
	}
	for _, intent := range intents {
		domain := string(intent.Domain)
		if narrowed[domain] {
			scope := strings.TrimSpace(intent.ScopeID)
			gen := strings.TrimSpace(intent.GenerationID)
			if scope == "" || gen == "" {
				continue
			}
			keyJSON, err := json.Marshal(waitingKey{ScopeID: scope, GenerationID: gen, ProducerDomain: domain})
			if err != nil {
				return fmt.Errorf("encode producer-terminal wake key: %w", err)
			}
			if err := wake(string(keyJSON)); err != nil {
				return err
			}
			// Cross-scope waiters parked scope-bound on this domain in
			// other scopes match no narrowed key; wake them by domain.
			// One statement per acked domain, deduped below.
			if _, ok := crossScoped[domain]; !ok {
				crossScoped[domain] = struct{}{}
				if err := wakeProducerTerminalCrossScope(ctx, database, instruments, domain, now); err != nil {
					return err
				}
			}
		}
		if producers[intent.Domain] {
			if keyJSON := waitingProducerDomainKey(domain); keyJSON != "" {
				if err := wake(keyJSON); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
