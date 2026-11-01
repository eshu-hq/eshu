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

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// supplyChainImpactFanOutReadTimeout bounds one holder-read statement. The read
// is an index scan measured at about 5 ms for one changed CVE and about 130 ms
// for a 200-CVE chunk; ten seconds stops a regressed plan from pinning a
// connection and a snapshot, and the pass fails (and is retried) rather than
// proceeding without its holders.
const supplyChainImpactFanOutReadTimeout = 10 * time.Second

// setSupplyChainImpactFanOutReadTimeoutQuery scopes the statement timeout to the
// read transaction, so a pooled connection never carries it away.
const setSupplyChainImpactFanOutReadTimeoutQuery = `SELECT set_config('statement_timeout', $1, true)`

// findSupplyChainImpactHoldersQuery returns the scopes that hold an active
// finding row for any of the given CVE ids, other than the asking scope. It
// reads the writer's own output through fact_records_supply_chain_impact_lookup_idx
// (leading expression payload->>'cve_id', partial on the fact kind and
// is_tombstone = FALSE), so the fact kind is a literal here: a bound parameter
// would not prove the partial-index predicate under a generic plan. Joining the
// scope's active generation drops rows left behind by superseded generations.
// The operator suppression scope is a holder like any other: its copies are
// ordinary finding rows that carry cve_id.
const findSupplyChainImpactHoldersQuery = `
SELECT DISTINCT finding.scope_id
FROM fact_records AS finding
JOIN ingestion_scopes AS scope
  ON scope.scope_id = finding.scope_id
 AND scope.active_generation_id = finding.generation_id
WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND finding.is_tombstone = FALSE
  AND finding.payload->>'cve_id' = ANY($1::text[])
  AND finding.scope_id <> $2
ORDER BY finding.scope_id
`

// reopenSupplyChainImpactHolderQuery reopens one holder's supply_chain_impact
// item at the scope's CURRENT active generation, in one autocommit statement
// that holds at most one row lock. The CTE locks the row in work_item_id order;
// the UPDATE evaluates every CASE on the locked row, so a row that committed
// succeeded while the statement waited is reopened rather than lost. The SET
// list matches scheduleCrossScopeMaterializationReplayQuery, including the
// container-image-identity authorization columns the claim path reads. A
// pending or retrying row is classified and left untouched, so a queued holder
// costs no row version; a claimed or running row gets the durable replay bit the
// migration 093 trigger turns into one rerun. The origin stamp is overwritten on
// every row the statement writes. Arguments: $1 holder scope id, $2 now, $3
// stamp JSON.
const reopenSupplyChainImpactHolderQuery = `
WITH target AS MATERIALIZED (
    SELECT work.work_item_id,
           work.status AS prior_status,
           work.cross_scope_replay_required AS prior_flag
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = work.scope_id
     AND scope.active_generation_id = work.generation_id
    WHERE work.scope_id = $1
      AND work.stage = 'reducer'
      AND work.domain = 'supply_chain_impact'
      AND work.status IN ('succeeded', 'claimed', 'running', 'pending', 'retrying')
    ORDER BY work.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF work
), moved AS (
    UPDATE fact_work_items AS work
    SET status = CASE WHEN work.status = 'succeeded' THEN 'pending' ELSE work.status END,
        attempt_count = CASE WHEN work.status = 'succeeded' THEN 0 ELSE work.attempt_count END,
        container_image_identity_v2_authorized_status = CASE
            WHEN work.status = 'succeeded' AND work.container_image_identity_v2_required THEN 'pending'
            ELSE work.container_image_identity_v2_authorized_status
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN work.status = 'succeeded' AND work.container_image_identity_v3_required THEN 'pending'
            ELSE work.container_image_identity_v3_authorized_status
        END,
        lease_owner = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.lease_owner END,
        claim_until = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.claim_until END,
        visible_at = CASE WHEN work.status = 'succeeded' THEN $2 ELSE work.visible_at END,
        next_attempt_at = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.next_attempt_at END,
        updated_at = $2,
        reopened_at = CASE WHEN work.status = 'succeeded' THEN $2 ELSE work.reopened_at END,
        cross_scope_replay_required = CASE
            WHEN work.status IN ('claimed', 'running') THEN TRUE
            ELSE work.cross_scope_replay_required
        END,
        failure_class = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_class END,
        failure_message = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_message END,
        failure_details = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_details END,
        payload = jsonb_set(work.payload, '{` + supplychaincore.PayloadKeyFanOutOrigin + `}', $3::jsonb, TRUE)
    FROM target
    WHERE work.work_item_id = target.work_item_id
      AND (work.status = 'succeeded'
           OR (work.status IN ('claimed', 'running') AND NOT work.cross_scope_replay_required))
    RETURNING work.work_item_id
)
SELECT target.prior_status,
       EXISTS (SELECT 1 FROM moved WHERE moved.work_item_id = target.work_item_id) AS rewritten
FROM target
ORDER BY target.work_item_id COLLATE "C"
`

// activeGenerationSupplyChainImpactQuery resolves the holder's active
// generation for the enqueue path.
const activeGenerationSupplyChainImpactQuery = `
SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1
`

// recordSupplyChainImpactFanOutStateQuery updates the calling pass's own item,
// fenced exactly like its acknowledgement: the lease owner, a live lease, and the
// attempt it claimed. $1 now, $2 work item id, $3 lease owner, $4 last attempt,
// $5 the new holder list as JSON (NULL with $6 set deletes the list), $6 whether
// to touch the list at all, $7 whether to consume the origin stamp, $8 the stamp
// the pass saw, $9 whether to clear the owed marker. The list is rewritten only when $6 is true, so an empty list is
// never "clear" by accident; a non-empty list also sets the durable replay bit so
// the item's success acknowledgement returns it to pending and the next claim
// drains the list. The stamp is deleted only while it still equals $8, so a newer
// stamp written since the claim survives.
const recordSupplyChainImpactFanOutStateQuery = `
UPDATE fact_work_items
SET payload = (CASE
                   WHEN NOT $6 THEN payload
                   WHEN $5::jsonb IS NULL THEN payload - '` + supplychaincore.PayloadKeyFanOutRemaining + `'
                   ELSE payload || jsonb_build_object('` + supplychaincore.PayloadKeyFanOutRemaining + `', $5::jsonb)
               END)
               - CASE WHEN $7 AND payload->'` + supplychaincore.PayloadKeyFanOutOrigin + `' = $8::jsonb
                      THEN '` + supplychaincore.PayloadKeyFanOutOrigin + `' ELSE '' END
               - CASE WHEN $9 THEN '` + supplychaincore.PayloadKeyFanOutOwed + `' ELSE '' END,
    cross_scope_replay_required = cross_scope_replay_required OR ($6 AND $5::jsonb IS NOT NULL),
    updated_at = $1
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $4
`

// SupplyChainImpactFanOutStore is the Postgres implementation of
// supplychaincore.SupplyChainImpactFanOut (#9695).
type SupplyChainImpactFanOutStore struct {
	// Beginner opens the short read-only transaction a holder read runs in.
	Beginner db.Beginner
	// DB runs the autocommit reopen and state statements.
	DB db.ExecQueryer
	// LeaseOwner is the reducer worker's lease owner, the same value its queue
	// uses, so the state update is fenced like an acknowledgement.
	LeaseOwner string
	// Now supplies the reopen timestamp; nil uses time.Now in UTC.
	Now func() time.Time
	// ReadTimeout overrides the holder-read statement timeout; zero uses
	// supplyChainImpactFanOutReadTimeout. Tests shorten it.
	ReadTimeout time.Duration
}

func (s SupplyChainImpactFanOutStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// FindHolderScopes returns the scopes other than ownScopeID that hold an active
// finding row for any of cveIDs, sorted. It runs as one read-only transaction
// under a statement timeout and fails with
// supplychaincore.ErrFanOutReadTimeout when the timeout fires.
func (s SupplyChainImpactFanOutStore) FindHolderScopes(
	ctx context.Context,
	ownScopeID string,
	cveIDs []string,
) ([]string, error) {
	if len(cveIDs) == 0 {
		return nil, nil
	}
	return s.readScopeIDs(ctx, findSupplyChainImpactHoldersQuery, cveIDs, ownScopeID)
}

// readScopeIDs runs one scope-id query as a short read-only transaction under the
// bounded statement timeout and returns the ids in query order. Every fan-out
// read goes through it, so each has the same timeout, the same typed timeout
// error, and the same retry semantics.
func (s SupplyChainImpactFanOutStore) readScopeIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	if s.Beginner == nil {
		return nil, errors.New("supply chain impact fan-out beginner is required")
	}
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = supplyChainImpactFanOutReadTimeout
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin supply chain impact fan-out read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("set supply chain impact fan-out read read-only: %w", err)
	}
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("set supply chain impact fan-out read timeout: %w", err)
	}
	_ = rows.Close()
	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact fan-out scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return scopes, nil
}

// classifyFanOutReadError wraps a statement-timeout failure (SQLSTATE 57014) or
// a context deadline in supplychaincore.ErrFanOutReadTimeout.
func classifyFanOutReadError(err error) error {
	var pgErr *pgconn.PgError
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && pgErr.Code == "57014") {
		return fmt.Errorf("%w: %v", supplychaincore.ErrFanOutReadTimeout, err)
	}
	return fmt.Errorf("find supply chain impact holders: %w", err)
}

// ReopenHolder reopens the holder's supply_chain_impact item at its current
// active generation, or enqueues one when that generation has none.
func (s SupplyChainImpactFanOutStore) ReopenHolder(
	ctx context.Context,
	holderScopeID string,
	origin supplychaincore.FanOutOrigin,
) (supplychaincore.FanOutOutcome, error) {
	if s.DB == nil {
		return "", errors.New("supply chain impact fan-out database is required")
	}
	stamp, err := json.Marshal(map[string]string{
		"scope_id":      origin.SourceScopeID,
		"generation_id": origin.SourceGenerationID,
		"intent_id":     origin.SourceIntentID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal supply chain impact fan-out stamp: %w", err)
	}
	now := s.now()
	rows, err := s.DB.QueryContext(ctx, reopenSupplyChainImpactHolderQuery, holderScopeID, now, string(stamp))
	if err != nil {
		return "", fmt.Errorf("reopen supply chain impact holder: %w", err)
	}
	var matched int
	outcome := supplychaincore.FanOutPendingNoop
	for rows.Next() {
		var (
			prior     string
			rewritten bool
		)
		if err := rows.Scan(&prior, &rewritten); err != nil {
			_ = rows.Close()
			return "", fmt.Errorf("scan supply chain impact holder reopen: %w", err)
		}
		matched++
		switch {
		case prior == "succeeded" && rewritten:
			outcome = supplychaincore.FanOutReopened
		case (prior == "claimed" || prior == "running") && rewritten && outcome != supplychaincore.FanOutReopened:
			outcome = supplychaincore.FanOutReplayFlagged
		}
	}
	closeErr := rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate supply chain impact holder reopen: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close supply chain impact holder reopen: %w", closeErr)
	}
	if matched > 0 {
		return outcome, nil
	}
	return s.enqueueMissingHolder(ctx, holderScopeID, origin, stamp, now)
}

// enqueueMissingHolder creates the holder's supply_chain_impact item when its
// active generation has none (the projector enqueues one only when the
// generation's own facts name a trigger). A holder with no active generation, or
// whose item already exists in a state the reopen does not move (a dead letter),
// is left alone and reported as a no-op: a dead letter is already loud and an
// operator replays it.
func (s SupplyChainImpactFanOutStore) enqueueMissingHolder(
	ctx context.Context,
	holderScopeID string,
	origin supplychaincore.FanOutOrigin,
	stamp []byte,
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
		return supplychaincore.FanOutPendingNoop, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(stamp, &payload); err != nil {
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

// UpdateFanOutState applies state to the calling pass's own queue item, fenced
// on its lease like the acknowledgement. A lost lease is an error: the pass
// failing is correct, because another worker now owns the item and will redo it.
func (s SupplyChainImpactFanOutStore) UpdateFanOutState(
	ctx context.Context,
	intent reducercontract.Intent,
	state supplychaincore.FanOutState,
) error {
	if s.DB == nil {
		return errors.New("supply chain impact fan-out database is required")
	}
	var remaining any
	if state.ReplaceRemaining && len(state.Remaining) > 0 {
		encoded, err := json.Marshal(state.Remaining)
		if err != nil {
			return fmt.Errorf("marshal supply chain impact fan-out continuation: %w", err)
		}
		remaining = string(encoded)
	}
	var expected any
	if state.ConsumeOrigin && state.ExpectedOrigin != nil {
		encoded, err := json.Marshal(state.ExpectedOrigin)
		if err != nil {
			return fmt.Errorf("marshal supply chain impact fan-out stamp: %w", err)
		}
		expected = string(encoded)
	}
	result, err := s.DB.ExecContext(ctx, recordSupplyChainImpactFanOutStateQuery,
		s.now(), intent.IntentID, s.LeaseOwner, claimedAtValue(intent), remaining, state.ReplaceRemaining, state.ConsumeOrigin, expected, state.ClearOwed)
	if err != nil {
		return fmt.Errorf("record supply chain impact fan-out state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record supply chain impact fan-out state rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("record supply chain impact fan-out state: lease on %q is no longer held", intent.IntentID)
	}
	return nil
}
