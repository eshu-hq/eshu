// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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
		return classifyWaitingInsertError(err, intent.IntentID, string(entry.WaitsForKind), keyJSON)
	}
	return nil
}

// wakeProducerTerminalCrossScopeQuery reopens scope-bound producer-terminal
// waits naming one producer domain, in any scope, whose failure class
// declared a cross-scope provider, and deletes their wait rows. $1 is the
// wake timestamp, $2 the producer domain, $3 the cross-scope failure
// classes (text[]). A cross-scope waiter (the catalog runtime correlation,
// keyed to the catalog scope while its fence reads the correlated
// repository scopes' workload rows; the #7816/#11053 pattern the contention
// proofs pin) commits its provider in a scope the keyed wake never names
// and the domain-only wake never matches, so without this leg it sleeps to
// the 10-minute backstop. Same-scope-generation classes are excluded by the
// failure_class filter (#11114): their keyed wake is byte-identical to the
// park key, so a foreign ack reopening them is only a spurious claim,
// probe, Fail, and re-park. failure_class on a wait row is set at park
// time and never rewritten (the insert upsert touches only updated_at).
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
      AND failure_class = ANY($3::text[])
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
      AND wait.failure_class = ANY($3::text[])
    RETURNING 1
)
SELECT (SELECT count(*) FROM reopened), (SELECT count(*) FROM cleared)
`

// wakeProducerTerminalCrossScope reopens every scope-bound wait parked on
// one producer domain whose failure class is in classes. Site and
// telemetry mirror wakeWaitingByKey.
func wakeProducerTerminalCrossScope(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	domain string,
	classes []string,
	now time.Time,
) error {
	var reopenedCount, clearedCount int64
	if err := querySingleRow(ctx, database, wakeProducerTerminalCrossScopeQuery,
		[]any{now, domain, array.StringArray(classes)}, &reopenedCount, &clearedCount); err != nil {
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
// never fan out. Scope-bound waits whose provider is cross-scope wake by the
// acked domain in any scope, filtered to the failure classes that declared
// a cross-scope provider; a domain with none issues no cross-scope
// statement (#11114). Domain-only #11000 waits wake keyed to the acked
// producer domain alone when that domain is a CompletionEdges producer,
// regardless of the acked scope. A waiter that parks after this Ack is
// covered by the set-aside re-check; one that parked before and whose
// provider committed without an Ack is covered by the audit fallback.
func wakeProducerTerminalAfterAck(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intents []reducercontract.Intent,
	now time.Time,
) error {
	return wakeProducerTerminalAfterAckWithPlan(ctx, database, instruments, intents, now,
		defaultProducerTerminalWakePlan())
}

// wakeProducerTerminalAfterAckWithPlan runs the post-ack wake against an
// explicit plan, so a test can seed a mis-declared locality and watch the
// wake it loses.
func wakeProducerTerminalAfterAckWithPlan(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intents []reducercontract.Intent,
	now time.Time,
	plan producerTerminalWakePlan,
) error {
	narrowed := plan.narrowed
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
			// other scopes match no narrowed key; wake them by domain,
			// limited to the classes that declared a cross-scope
			// provider. One statement per acked domain, deduped below;
			// none for a domain whose waiters are all same-scope.
			classes := plan.crossScopeClasses[domain]
			if _, ok := crossScoped[domain]; !ok && len(classes) > 0 {
				crossScoped[domain] = struct{}{}
				if err := wakeProducerTerminalCrossScope(ctx, database, instruments, domain, classes, now); err != nil {
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

// WaitingProviderLocality names where the provider of a narrowed
// producer-terminal wait commits, relative to the wait's own scope and
// generation (#11114). It decides which acks may reopen the wait: a
// same-scope-generation wait reopens only on the keyed wake for its own
// (scope, generation, producer domain), while a cross-scope wait also
// reopens on the cross-scope leg, which matches the producer domain in any
// scope. The zero value is not a valid locality: every narrowed
// producer-terminal row of WaitingDeferralMap must declare one, and the
// completeness test fails when a row leaves it empty.
type WaitingProviderLocality string

// Closed provider-locality set (#11114).
const (
	// WaitingProviderSameScopeGeneration means the handler's readiness probe
	// reads the producer rows of the waiter's own scope and generation, so
	// the keyed wake (byte-identical to the park key) is the only wake that
	// can satisfy it. A foreign scope's ack never reopens such a wait.
	WaitingProviderSameScopeGeneration WaitingProviderLocality = "same_scope_generation"
	// WaitingProviderCrossScope means the handler's readiness probe reads
	// producer rows in scopes other than the one the wait is keyed to (the
	// catalog runtime fence reads the correlated repository scopes), so the
	// keyed wake never matches and the cross-scope leg must reopen it.
	WaitingProviderCrossScope WaitingProviderLocality = "cross_scope"
)

// Valid reports whether l is one of the two closed provider localities.
func (l WaitingProviderLocality) Valid() bool {
	switch l {
	case WaitingProviderSameScopeGeneration, WaitingProviderCrossScope:
		return true
	}
	return false
}

// producerTerminalWakePlan is the post-ack wake plan derived once from the
// deferral map: which producer domains narrow a producer-terminal wait, and
// for each such domain the failure classes whose waits the cross-scope leg
// may reopen.
type producerTerminalWakePlan struct {
	// narrowed holds the producer domains some narrowed producer-terminal
	// deferral names; a terminal Ack of one of them issues the keyed wake.
	narrowed map[string]bool
	// crossScopeClasses maps a narrowed producer domain to the sorted
	// failure classes that declared a cross-scope provider. A domain absent
	// here issues no cross-scope statement.
	crossScopeClasses map[string][]string
}

// newProducerTerminalWakePlan builds the wake plan from deferral rows. A
// narrowed row with an invalid locality is planned as cross-scope: an
// undeclared row over-wakes (a hint the handler re-checks) rather than
// losing a wake. The completeness test still fails on such a row.
func newProducerTerminalWakePlan(entries []WaitingDeferral) producerTerminalWakePlan {
	plan := producerTerminalWakePlan{
		narrowed:          map[string]bool{},
		crossScopeClasses: map[string][]string{},
	}
	for _, entry := range entries {
		if entry.WaitsForKind != WaitingKindProducerTerminal || entry.ProducerDomain == "" {
			continue
		}
		plan.narrowed[entry.ProducerDomain] = true
		if entry.ProviderLocality == WaitingProviderSameScopeGeneration {
			continue
		}
		classes := plan.crossScopeClasses[entry.ProducerDomain]
		if !slices.Contains(classes, entry.FailureClass) {
			classes = append(classes, entry.FailureClass)
		}
		plan.crossScopeClasses[entry.ProducerDomain] = classes
	}
	for domain := range plan.crossScopeClasses {
		slices.Sort(plan.crossScopeClasses[domain])
	}
	return plan
}

// defaultProducerTerminalWakePlan returns the wake plan for
// WaitingDeferralMap, built once: every terminal reducer Ack consults it, so
// rebuilding it per Ack would be allocation churn on the ack path. Callers
// must not mutate the returned maps or slices.
func defaultProducerTerminalWakePlan() producerTerminalWakePlan {
	producerTerminalWakePlanOnce.Do(func() {
		cachedProducerTerminalWakePlan = newProducerTerminalWakePlan(WaitingDeferralMap())
	})
	return cachedProducerTerminalWakePlan
}

var (
	producerTerminalWakePlanOnce   sync.Once
	cachedProducerTerminalWakePlan producerTerminalWakePlan
)
