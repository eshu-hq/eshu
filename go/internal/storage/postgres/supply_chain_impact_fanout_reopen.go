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

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// reopenSupplyChainImpactMatchedStatuses is the status set the reopen reads for
// a holder: every state it classifies. A row in any other state (failed,
// superseded) is not matched, and a holder with no matched row is enqueued. The
// queued-holder prefilter (findQueuedSupplyChainImpactHoldersQuery) reads the
// same set, so the two cannot disagree on which rows exist.
const reopenSupplyChainImpactMatchedStatuses = `('succeeded', 'claimed', 'running', 'pending', 'retrying', 'dead_letter')`

// reopenSupplyChainImpactDeadLetter is true for a dead letter the fan-out may
// reopen (#11147): its failure class is not in the admin unsafe-replay set
// ($4, from recovery.UnsafeReplayFailureClasses, the one source of that set).
// A NULL class reopens.
const reopenSupplyChainImpactDeadLetter = `(work.status = 'dead_letter' AND NOT (COALESCE(work.failure_class, '') = ANY($4::text[])))`

// reopenSupplyChainImpactToPending is true for a row the reopen returns to
// pending: a succeeded row, or a dead letter the fan-out may reopen.
const reopenSupplyChainImpactToPending = `(work.status = 'succeeded' OR ` + reopenSupplyChainImpactDeadLetter + `)`

// reopenSupplyChainImpactWidenQueued is true for a pending or retrying row
// whose next pass does not cover the incoming change (#10972): the reopen
// merges the change into its stamp and writes nothing else.
var reopenSupplyChainImpactWidenQueued = `(work.status IN ('pending', 'retrying') AND NOT ` +
	fanOutQueuedRowCoversSQL("$5::jsonb", "$6::boolean") + `)`

// reopenSupplyChainImpactWidenFlagged is true for a claimed or running row
// whose replay flag is already on and whose stamp set does not name the
// incoming change (#10972): its one rerun must derive the union.
var reopenSupplyChainImpactWidenFlagged = `(work.status IN ('claimed', 'running') AND work.cross_scope_replay_required AND NOT ` +
	fanOutStampCoversSQL(fanOutStampSQL, "$5::jsonb", "$6::boolean") + `)`

// reopenSupplyChainImpactSetsFlag is true for a claimed or running row the
// reopen flags for one rerun.
const reopenSupplyChainImpactSetsFlag = `(work.status IN ('claimed', 'running') AND NOT work.cross_scope_replay_required)`

// reopenSupplyChainImpactHolderQuery reopens one holder's supply_chain_impact
// item at the scope's CURRENT active generation, in one autocommit statement
// that holds at most one row lock. The CTE locks the row in work_item_id order;
// the UPDATE evaluates every CASE on the locked row, so a row that committed
// succeeded while the statement waited is reopened rather than lost, and a dead
// letter another session reopened while it waited reads as pending. The SET
// list matches scheduleCrossScopeMaterializationReplayQuery, including the
// container-image-identity authorization columns the claim path reads.
//
// Per state: a succeeded row returns to pending, its stamp UNIONED with the
// incoming origin (fanOutMergedStampSQL), so a set a mid-pass widen told it
// about survives the consume fence and is never dropped (#11613: without the
// union a second origin's CVE set vanished when the succeeded row already
// carried one). A claimed or running row gets the durable replay bit the
// migration 093 trigger turns into one rerun, and its stamp is MERGED with the
// incoming one, so a set the running pass was told about is never dropped.
// A pending or retrying row whose next pass covers the change
// (fanOutQueuedRowCoversSQL) is classified and left untouched, so it costs no
// row version; one that does not is widened (#10972): only payload.fanout_origin
// is written, never status, attempts, lease, failure fields, visible_at, or
// updated_at, because the claim orders by updated_at and a bump would push a
// busy holder to the back on every fan-out. A claimed or running row whose flag
// is already on is widened the same way when its stamp does not name the
// change, and left untouched when it does or when it has no stamp object (a
// flag set by something other than a fan-out: its rerun is a genuine full
// pass, which a stamp would narrow). A dead letter whose class is not
// unsafe to replay (#11147) returns to pending as a full genuine pass: attempts
// zeroed, lease, retry, and failure fields cleared, the identity authorization
// columns set the way the admin replay sets them, the replay bit off, and the
// origin stamp REMOVED rather than written, because a stamped attempt-1 pass
// runs as a CVE-scoped one hop and would hide the unfinished genuine pass. Its
// fanout_owed marker and fanout_remaining list stay, so the reopened pass still
// fans out what it owes. A dead letter in an unsafe class is locked and
// classified but not written.
//
// The SELECT reports whether a succeeded reopen met every PR1 freshness
// condition (shadowed): the origin stamp carries a positive fencing token
// and a non-empty CVE set, the item owes nothing, and the scope's durable
// holder watermark names this generation with a strictly newer
// evidence-complete token. PR1 still reopens the row; the flag only changes
// the reported outcome to shadow_would_skip so the would-skip share is
// counted. Arguments: $1 holder scope id, $2 now, $3 stamp
// JSON, $4 the unsafe-replay failure classes, $5 the stamp ids as a jsonb
// array and $6 whether they are bounded (fanOutChangeArgs).
// reopenShadowedSQL is TRUE for a succeeded row that meets every PR1
// freshness condition, so its reopen reports shadow_would_skip while the row
// still goes pending (#11613). Every arm fails closed: a token-less, set-less,
// or corrupt origin, any owed debt, a missing or stale
// watermark, and any non-succeeded prior status all read FALSE, and the row
// reopens exactly as it did before. NULL-safe throughout: a NULL arm would
// drop the holder's report, never skip it. The token regex guards the cast:
// a non-numeric fencing_token reads 0 instead of erroring the reopen. The
// cves arm is CASE-fenced because jsonb_array_length on a non-array errors;
// only the proven-array arm evaluates it.
const reopenShadowedSQL = `(target.prior_status = 'succeeded'
    AND target.origin_token > 0
    AND (target.item_payload->'` + supplychaincore.PayloadKeyFanOutOwed + `' IS DISTINCT FROM 'true'::jsonb)
    AND (target.item_payload->'` + supplychaincore.PayloadKeyFanOutRemaining + `' IS NULL
         OR target.item_payload->'` + supplychaincore.PayloadKeyFanOutRemaining + `' = '[]'::jsonb)
    AND (CASE WHEN jsonb_typeof($3::jsonb->'` + supplychaincore.FanOutStampCVEKey + `') = 'array'
              THEN jsonb_array_length($3::jsonb->'` + supplychaincore.FanOutStampCVEKey + `') > 0
              ELSE FALSE END)
    AND target.watermark_complete IS TRUE
    AND target.watermark_generation = target.item_generation
    AND target.watermark_token > target.origin_token)`

var reopenSupplyChainImpactHolderQuery = `
WITH target AS MATERIALIZED (
    SELECT work.work_item_id,
           work.status AS prior_status,
           work.cross_scope_replay_required AS prior_flag,
           COALESCE(work.failure_class, '') AS prior_failure_class,
           work.generation_id AS item_generation,
           work.payload AS item_payload,
           CASE WHEN ($3::jsonb->>'` + supplychaincore.FanOutStampFencingTokenKey + `') ~ '^[0-9]+$'
                THEN (($3::jsonb->>'` + supplychaincore.FanOutStampFencingTokenKey + `')::bigint) ELSE 0 END AS origin_token,
           wm.watermark_token AS watermark_token,
           wm.watermark_generation_id AS watermark_generation,
           wm.watermark_evidence_complete AS watermark_complete
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = work.scope_id
     AND scope.active_generation_id = work.generation_id
    LEFT JOIN supply_chain_impact_scope_pass_state AS wm
      ON wm.scope_id = work.scope_id
    WHERE work.scope_id = $1
      AND work.stage = 'reducer'
      AND work.domain = 'supply_chain_impact'
      AND work.status IN ` + reopenSupplyChainImpactMatchedStatuses + `
    ORDER BY work.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF work
), moved AS (
    UPDATE fact_work_items AS work
    SET status = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN 'pending' ELSE work.status END,
        attempt_count = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN 0 ELSE work.attempt_count END,
        container_image_identity_v2_authorized_status = CASE
            WHEN work.status = 'succeeded' AND work.container_image_identity_v2_required THEN 'pending'
            WHEN ` + reopenSupplyChainImpactDeadLetter + ` THEN
                CASE WHEN work.container_image_identity_v2_required THEN 'pending' ELSE '' END
            ELSE work.container_image_identity_v2_authorized_status
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN work.status = 'succeeded' AND work.container_image_identity_v3_required THEN 'pending'
            WHEN ` + reopenSupplyChainImpactDeadLetter + ` THEN
                CASE WHEN work.container_image_identity_v3_required THEN 'pending' ELSE '' END
            ELSE work.container_image_identity_v3_authorized_status
        END,
        lease_owner = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.lease_owner END,
        claim_until = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.claim_until END,
        visible_at = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN $2 ELSE work.visible_at END,
        next_attempt_at = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.next_attempt_at END,
        updated_at = CASE
            WHEN ` + reopenSupplyChainImpactToPending + ` OR ` + reopenSupplyChainImpactSetsFlag + ` THEN $2
            ELSE work.updated_at
        END,
        reopened_at = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN $2 ELSE work.reopened_at END,
        cross_scope_replay_required = CASE
            WHEN work.status IN ('claimed', 'running') THEN TRUE
            WHEN ` + reopenSupplyChainImpactDeadLetter + ` THEN FALSE
            ELSE work.cross_scope_replay_required
        END,
        failure_class = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.failure_class END,
        failure_message = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.failure_message END,
        failure_details = CASE WHEN ` + reopenSupplyChainImpactToPending + ` THEN NULL ELSE work.failure_details END,
        payload = CASE
            WHEN ` + reopenSupplyChainImpactDeadLetter + ` THEN work.payload - '` + supplychaincore.PayloadKeyFanOutOrigin + `'
            WHEN work.status = 'succeeded' THEN jsonb_set(work.payload, '{` + supplychaincore.PayloadKeyFanOutOrigin + `}',
                COALESCE(` + fanOutMergedStampSQL("$3::jsonb", "$5::jsonb", "$6::boolean") + `, $3::jsonb), TRUE)
            ELSE jsonb_set(work.payload, '{` + supplychaincore.PayloadKeyFanOutOrigin + `}',
                COALESCE(` + fanOutMergedStampSQL("$3::jsonb", "$5::jsonb", "$6::boolean") + `, $3::jsonb), TRUE)
        END
    FROM target
    WHERE work.work_item_id = target.work_item_id
      AND (` + reopenSupplyChainImpactToPending + `
           OR ` + reopenSupplyChainImpactSetsFlag + `
           OR ` + reopenSupplyChainImpactWidenQueued + `
           OR ` + reopenSupplyChainImpactWidenFlagged + `)
    RETURNING work.work_item_id
)
SELECT target.prior_status,
       EXISTS (SELECT 1 FROM moved WHERE moved.work_item_id = target.work_item_id) AS rewritten,
       target.prior_flag,
       target.prior_failure_class,
       ` + reopenShadowedSQL + ` AS shadowed
FROM target
ORDER BY target.work_item_id COLLATE "C"
`

// activeGenerationSupplyChainImpactQuery resolves the holder's active
// generation for the enqueue path.
const activeGenerationSupplyChainImpactQuery = `
SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1
`

// ReopenHolderDetailed implements supplychaincore.SupplyChainImpactFanOut: it
// reopens the holder's supply_chain_impact item at its current active
// generation, or enqueues one when that generation has none, and names a dead
// letter's prior failure class. The stamp carries the origin's CVE set (#9740)
// and, when the origin names one, its fencing token (#11613); the enqueue
// path stamps none, so a generation that never had an item runs its
// establishing pass full, and a reopened dead letter carries none (#11147), so
// it runs a full genuine pass. A succeeded reopen that meets every PR1
// freshness condition reports shadow_would_skip while the row still goes
// pending. A dead letter in an unsafe-replay class
// (recovery.UnsafeReplayFailureClasses) is left dead and reported skipped.
func (s SupplyChainImpactFanOutStore) ReopenHolderDetailed(
	ctx context.Context,
	holderScopeID string,
	origin supplychaincore.FanOutOrigin,
) (supplychaincore.FanOutReopen, error) {
	if s.DB == nil {
		return supplychaincore.FanOutReopen{}, errors.New("supply chain impact fan-out database is required")
	}
	canonical := origin
	canonical.CVEIDs = supplychaincore.CapFanOutStampCVEIDs(origin.CVEIDs)
	stamp, err := supplychaincore.FanOutStampJSON(canonical)
	if err != nil {
		return supplychaincore.FanOutReopen{}, fmt.Errorf("marshal supply chain impact fan-out stamp: %w", err)
	}
	cves, bounded, err := fanOutChangeArgs(canonical.CVEIDs)
	if err != nil {
		return supplychaincore.FanOutReopen{}, err
	}
	now := s.now()
	rows, err := s.DB.QueryContext(ctx, reopenSupplyChainImpactHolderQuery,
		holderScopeID, now, string(stamp), recovery.UnsafeReplayFailureClasses(), cves, bounded)
	if err != nil {
		return supplychaincore.FanOutReopen{}, fmt.Errorf("reopen supply chain impact holder: %w", err)
	}
	var (
		matched int
		result  = supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutPendingNoop}
	)
	for rows.Next() {
		var (
			prior, priorClass    string
			rewritten, priorFlag bool
			shadowed             bool
		)
		if err := rows.Scan(&prior, &rewritten, &priorFlag, &priorClass, &shadowed); err != nil {
			_ = rows.Close()
			return supplychaincore.FanOutReopen{}, fmt.Errorf("scan supply chain impact holder reopen: %w", err)
		}
		matched++
		if next := classifyHolderReopen(prior, rewritten, priorFlag, priorClass, shadowed); reopenRank(next.Outcome) > reopenRank(result.Outcome) {
			result = next
		}
	}
	closeErr := rows.Close()
	if err := rows.Err(); err != nil {
		return supplychaincore.FanOutReopen{}, fmt.Errorf("iterate supply chain impact holder reopen: %w", err)
	}
	if closeErr != nil {
		return supplychaincore.FanOutReopen{}, fmt.Errorf("close supply chain impact holder reopen: %w", closeErr)
	}
	if matched > 0 {
		return result, nil
	}
	outcome, err := s.enqueueMissingHolder(ctx, holderScopeID, origin, now)
	return supplychaincore.FanOutReopen{Outcome: outcome}, err
}

// classifyHolderReopen names what the reopen statement did to one row. A dead
// letter with no failure class reports supplyChainImpactDeadLetterUnclassified,
// the label the dead-letter gauge uses, so the counter, the log line, and the
// gauge name a NULL class the same way. A rewritten pending or retrying row, or
// a rewritten claimed or running row whose flag was already on, had only its
// stamp widened (#10972). A succeeded row the statement reports shadowed met
// every PR1 freshness condition: it still went pending (PR1 enables no skip),
// but the outcome counts the would-skip (#11613).
func classifyHolderReopen(prior string, rewritten, priorFlag bool, priorClass string, shadowed bool) supplychaincore.FanOutReopen {
	if prior == "dead_letter" && priorClass == "" {
		priorClass = supplyChainImpactDeadLetterUnclassified
	}
	switch {
	case prior == "succeeded" && rewritten && shadowed:
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutShadowWouldSkip}
	case prior == "succeeded" && rewritten:
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutReopened}
	case prior == "dead_letter" && rewritten:
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutDeadLetterReopened, PriorFailureClass: priorClass}
	case prior == "dead_letter":
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutDeadLetterSkipped, PriorFailureClass: priorClass}
	case (prior == "claimed" || prior == "running") && rewritten && !priorFlag:
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutReplayFlagged}
	case rewritten && prior != "succeeded":
		return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutStampWidened}
	}
	return supplychaincore.FanOutReopen{Outcome: supplychaincore.FanOutPendingNoop}
}

// reopenRank orders the per-row outcomes when a scope's active generation has
// more than one item, so the holder reports the strongest thing the statement
// did: a reopen beats a would-skip shadow (a genuinely reopened row did work
// the shadow row did not), the shadow beats a dead-letter reopen, either beats
// a skipped dead letter, and a skipped dead letter, which an operator must act
// on, beats a stamp widen, which still beats a no-op: a widened row wrote a
// row version and is not a no-op (#10972).
func reopenRank(outcome supplychaincore.FanOutOutcome) int {
	switch outcome {
	case supplychaincore.FanOutReopened:
		return 6
	case supplychaincore.FanOutShadowWouldSkip:
		return 5
	case supplychaincore.FanOutDeadLetterReopened:
		return 4
	case supplychaincore.FanOutReplayFlagged:
		return 3
	case supplychaincore.FanOutDeadLetterSkipped:
		return 2
	case supplychaincore.FanOutStampWidened:
		return 1
	case supplychaincore.FanOutPendingNoop, supplychaincore.FanOutEnqueued, supplychaincore.FanOutNoActiveGeneration:
		// The statement never classifies a row as enqueued or as having no
		// active generation; both come from the enqueue path, after it.
		return 0
	}
	return 0
}

// ReopenHolder is ReopenHolderDetailed reporting only the outcome.
func (s SupplyChainImpactFanOutStore) ReopenHolder(
	ctx context.Context,
	holderScopeID string,
	origin supplychaincore.FanOutOrigin,
) (supplychaincore.FanOutOutcome, error) {
	reopen, err := s.ReopenHolderDetailed(ctx, holderScopeID, origin)
	return reopen.Outcome, err
}

// ReopenSupplyChainImpactHolderInTransaction is ReopenHolder on the caller's
// transaction, stamped with the generation that changed. The commit-skew fence
// (eshu-hq/eshu#10055) uses it so a retraction and its holders' reopen commit
// together: a crash cannot leave a retracted generation whose holders were never
// told. It takes one holder's item row locks and no scope row, and the caller owns
// their duration, so reopen holders in a stable order.
func ReopenSupplyChainImpactHolderInTransaction(
	ctx context.Context,
	tx db.ExecQueryer,
	holderScopeID, sourceScopeID, sourceGenerationID string,
	now time.Time,
) error {
	store := SupplyChainImpactFanOutStore{DB: tx, Now: func() time.Time { return now }}
	_, err := store.ReopenHolder(ctx, holderScopeID, supplychaincore.FanOutOrigin{
		SourceScopeID:      sourceScopeID,
		SourceGenerationID: sourceGenerationID,
	})
	return err
}

// enqueueMissingHolder creates the holder's supply_chain_impact item when its
// active generation has none (the projector enqueues one only when the
// generation's own facts name a trigger). A holder with no active generation is
// reported as no_active_generation. An insert that conflicts means an item
// appeared after the reopen's read (a concurrent enqueue, so it is queued) or
// sits in a state the reopen does not read (failed or superseded); both are
// reported as a no-op. The enqueued item stamps the origin without its CVE set:
// this may be the generation's establishing pass, which must derive the whole
// finding set, so it always runs full.
func (s SupplyChainImpactFanOutStore) enqueueMissingHolder(
	ctx context.Context,
	holderScopeID string,
	origin supplychaincore.FanOutOrigin,
	now time.Time,
) (supplychaincore.FanOutOutcome, error) {
	rows, err := s.DB.QueryContext(ctx, activeGenerationSupplyChainImpactQuery, holderScopeID)
	if err != nil {
		return "", fmt.Errorf("read supply chain impact holder generation: %w", err)
	}
	var generationID string
	if rows.Next() {
		if err := rows.Scan(&generationID); err != nil {
			_ = rows.Close()
			return "", fmt.Errorf("scan supply chain impact holder generation: %w", err)
		}
	}
	closeErr := rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate supply chain impact holder generation: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close supply chain impact holder generation: %w", closeErr)
	}
	if strings.TrimSpace(generationID) == "" {
		return supplychaincore.FanOutNoActiveGeneration, nil
	}
	withoutCVEs := origin
	withoutCVEs.CVEIDs = nil
	encoded, err := supplychaincore.FanOutStampJSON(withoutCVEs)
	if err != nil {
		return "", fmt.Errorf("marshal supply chain impact fan-out stamp: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return "", fmt.Errorf("decode supply chain impact fan-out stamp: %w", err)
	}
	queue := NewReducerQueue(s.DB, s.LeaseOwner, 0)
	inserted, err := queue.enqueueReducerBatch(ctx, []runtime.ReducerIntent{{
		ScopeID:      holderScopeID,
		GenerationID: generationID,
		Domain:       reducercontract.DomainSupplyChainImpact,
		EntityKey:    "supply_chain_impact:" + holderScopeID,
		Reason:       "supply chain impact holder re-derivation after " + origin.SourceScopeID + " changed a finding",
		SourceSystem: "reducer",
		Payload:      map[string]any{supplychaincore.PayloadKeyFanOutOrigin: payload},
	}}, now)
	if err != nil {
		return "", fmt.Errorf("enqueue supply chain impact holder: %w", err)
	}
	if inserted > 0 {
		return supplychaincore.FanOutEnqueued, nil
	}
	return supplychaincore.FanOutPendingNoop, nil
}
