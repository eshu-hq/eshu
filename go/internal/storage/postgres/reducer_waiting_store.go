// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2blockkms"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2instance"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2usesprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/iaminstprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/rdsposture"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3logsto"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
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

// waitingKey lives in reducer_waiting_narrow.go with the narrowed corpus
// fields. Scope/generation keys stay coarse on purpose: the wake is a hint
// and the claim gate plus the handler re-check the exact requirement, so a
// wake never starts work early.

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
// deferral deserves one re-check), $9 producer collector kinds (text[]:
// the kinds the parked producer domain maps to, empty when unmapped).
//
// The domain-only producer branch replicates the handler quiescence rule
// for the mapped kind: zero registered scopes, or at least one
// quiescent-active scope, reads satisfied; an unmapped domain (empty $9)
// counts as satisfied. The probe shape mirrors
// producerScopeQuiescenceSQL (registered scopes of the kind, each flagged
// quiescent-active by an active generation with no live projector work);
// like the claim query's projector-drain fence, which shares the same
// columns without sharing the text, the two statements cannot be identical
// because this one correlates against the parked domain's kind rather than
// scanning an already-anchored scope.
// The generation_active branch of waitingSatisfiedQuery resolves on
// terminal truth, not only on activation (#11015): a failed or superseded
// generation never activates, so a waiter parked on it must observe the
// terminal generation (the handler re-checks the exact requirement) instead
// of sitting out the backstop. The projector Fail and Claim-path
// supersession wakes reopen already-parked waits synchronously; this
// predicate covers a waiter that parks after the provider committed, plus
// the audit fallback.
const waitingSatisfiedQuery = `
SELECT EXISTS (
    SELECT 1 FROM scope_generations AS gen
    WHERE $1 = 'generation_active'
      AND gen.scope_id = $2 AND gen.generation_id = $3 AND gen.status IN ('active', 'failed', 'superseded')
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
    WHERE $1 = 'producer_domain_terminal' AND $4 <> '' AND $2 <> ''
      AND producer.scope_id = $2 AND producer.generation_id = $3
      AND producer.stage = 'reducer' AND producer.domain = $4
      AND producer.status IN ('succeeded', 'failed', 'dead_letter', 'superseded')
) OR (
    $1 = 'producer_domain_terminal' AND $4 <> '' AND $2 = ''
    AND NOT (
        EXISTS (
            SELECT 1 FROM ingestion_scopes AS kind_scope
            WHERE kind_scope.collector_kind = ANY ($9::text[])
        ) AND NOT EXISTS (
            SELECT 1 FROM ingestion_scopes AS kind_scope
            WHERE kind_scope.collector_kind = ANY ($9::text[])
              AND kind_scope.active_generation_id IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1 FROM fact_work_items AS kind_work
                  WHERE kind_work.stage = 'projector'
                    AND kind_work.scope_id = kind_scope.scope_id
                    AND kind_work.status IN ('pending', 'retrying', 'claimed', 'running')
              )
        )
    )
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

// waitingSatisfied and querySingleRow live in reducer_waiting_narrow.go
// with the narrowed corpus branches.

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

// wakeGenerationActiveWaits reopens the generation_active waits parked on
// one scope generation (#10876, #11015): the projector Ack that activates
// it, the terminal projector Fail that retires it, and the projector Claim
// sweep that supersedes it. The wake is a hint: the claim gate and the
// handler re-check the exact requirement, so a spurious wake only re-parks.
// An empty key skips the wake, mirroring the phase Upsert guard: the wake
// statement would reject it and fail the caller, and no parked wait names a
// blank key.
func wakeGenerationActiveWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	scopeID, generationID string,
	now time.Time,
) error {
	wakeKey := waitingScopeGenerationKey(scopeID, generationID)
	if wakeKey == "" {
		return nil
	}
	if _, _, err := wakeWaitingByKey(ctx, database, instruments, site,
		WaitingKindGenerationActive, wakeKey, now); err != nil {
		return fmt.Errorf("wake generation_active waits: %w", err)
	}
	return nil
}

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

// producerTerminalParkDomains resolves the producer domains a cross-scope
// deferral parks on: the floor error's own unready set first, the
// CompletionEdges catalog derivation for the consumer when the error names
// none, and nothing when neither yields a domain (the caller then keeps the
// existing timer path instead of parking a wait that can never wake).
func producerTerminalParkDomains(cause error, consumer reducercontract.Domain) []reducercontract.Domain {
	var notReady crossscope.ProducerNotReadyError
	if errors.As(cause, &notReady) && len(notReady.ProducerDomains) > 0 {
		out := make([]reducercontract.Domain, 0, len(notReady.ProducerDomains))
		return append(out, notReady.ProducerDomains...)
	}
	var out []reducercontract.Domain
	for _, edge := range crossscope.CompletionEdges() {
		if edge.Consumer == consumer {
			out = append(out, edge.Producer)
		}
	}
	return out
}

// completionEdgeProducerSet is the CompletionEdges producer universe: the
// domains whose terminal reducer Ack can provide a parked producer wait.
// Consumers never commit a terminal row of their own, so they never wake.
func completionEdgeProducerSet() map[reducercontract.Domain]bool {
	out := map[reducercontract.Domain]bool{}
	for _, edge := range crossscope.CompletionEdges() {
		out[edge.Producer] = true
	}
	return out
}

// setAsideWaiting parks one deferred intent: it inserts the wait row and,
// when the provider already committed, reopens it at once so a waiter that
// registers after its provider never strands (the same lost-wake pattern as
// the #7597 Fail-path wake). It returns true when the wait is parked (or
// reopened at once) and false when no wait row applies.
//
// producerDomains carries the #11000 per-domain park set for the
// cross-scope producer class; every other class passes nil and keeps the
// legacy single-key path.
func setAsideWaiting(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	intent reducercontract.Intent,
	entry WaitingDeferral,
	producerDomains []reducercontract.Domain,
	now time.Time,
) (bool, error) {
	if entry.WakeSite == WaitingWakeSiteCounting || entry.WakeSite == WaitingWakeSiteNotAWait {
		return false, nil
	}
	if entry.WaitsForKind == WaitingKindProducerTerminal && entry.ProducerDomain == "" {
		return parkProducerTerminalWaits(ctx, database, instruments, intent, entry, producerDomains, now)
	}
	keyJSON, ok := waitingKeyFor(intent, entry)
	if !ok {
		return false, nil
	}
	return setAsideWaitingForKey(ctx, database, instruments, intent, entry, keyJSON, now)
}

// waitingPhaseDeferral builds one phase-commit map row: the waiter's
// canonical nodes commit with the producer's phase publish.
func waitingPhaseDeferral(class, save string) WaitingDeferral {
	return WaitingDeferral{
		FailureClass:  class,
		WaitsForKind:  WaitingKindPhaseCommitted,
		WakeSite:      WaitingWakeSiteInTx,
		ProvidingSave: save,
	}
}

// waitingPhaseDeferrals lists the phase-commit family: materialization
// domains whose readiness gate waits on the producer's canonical-node phase
// commit (the largest deferral family in the #10876 census).
func waitingPhaseDeferrals() []WaitingDeferral {
	phase := waitingPhaseDeferral
	save := "producer phase-commit upsert (graph_projection_phase_state)"
	return []WaitingDeferral{
		phase(secretsiam.SecretsIAMEndpointNotReadyFailureClass, save),
		phase(kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass, save),
		phase(reducer.GCPRelationshipNodesNotReadyFailureClass, save),
		phase(ec2instance.EC2InstanceIdentityNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipEC2InstanceNodesNotReadyFailureClass, save),
		phase(awscloud.AWSCloudImageNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipNodesNotReadyFailureClass, save),
		phase(reducer.AzureRelationshipNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanAssumeNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanPerformNodesNotReadyFailureClass, save),
		phase(iamescalation.IAMEscalationNodesNotReadyFailureClass, save),
		phase(iaminstprofile.IAMInstanceProfileRoleNodesNotReadyFailureClass, save),
		phase(obscoverage.ObservabilityCoverageNodesNotReadyFailureClass, save),
		phase(rdsposture.RDSPostureNodesNotReadyFailureClass, save),
		phase(secgroup.SecurityGroupReachabilityNodesNotReadyFailureClass, save),
		phase(reducer.WorkloadCloudRelationshipNodesNotReadyFailureClass, save),
		phase(ec2blockkms.EC2BlockDeviceKMSPostureNodesNotReadyFailureClass, save),
		phase(internetexposure.EC2InternetExposureNodesNotReadyFailureClass, save),
		phase(ec2usesprofile.EC2UsesProfileNodesNotReadyFailureClass, save),
		phase(s3grant.S3ExternalPrincipalGrantNodesNotReadyFailureClass, save),
		phase(internetexposure.S3InternetExposureNodesNotReadyFailureClass, save),
		phase(s3logsto.S3LogsToNodesNotReadyFailureClass, save),
		{
			FailureClass:  reducer.DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass,
			WaitsForKind:  WaitingKindPhaseCommitted,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "producer phase-commit upsert (graph_projection_phase_state)",
		},
	}
}
