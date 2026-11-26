// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// supplyChainImpactFanOutReadTimeout bounds one holder-read statement. The
// 5 ms (one CVE) and 130 ms (200-CVE chunk) calibration predates hot ids: on
// QA a hot id's shipped read ran 11.42 s cold (#10643), which is why a hot
// singleton whose true mass sits above the key-range gate reads as key-range
// statements (#10972 H2 A2) while at-or-under-gate singletons keep the
// shipped single statement. Ten seconds stops a regressed plan from pinning a
// connection and a snapshot, and the pass fails (and is retried) rather than
// proceeding without its holders.
const supplyChainImpactFanOutReadTimeout = 10 * time.Second

// setSupplyChainImpactFanOutReadTimeoutQuery scopes the statement timeout to the
// read transaction, so a pooled connection never carries it away.
const setSupplyChainImpactFanOutReadTimeoutQuery = `SELECT set_config('statement_timeout', $1, true)`

// FindSupplyChainImpactHoldersQuery returns the scopes that hold an active
// finding support matching any of the given lookup ids, other than the asking
// scope. A support matches on its cve_id, its advisory_id, or its stored
// advisory-group member set (#10408): a re-key retires group and member ids
// no replacement row names, and holders keyed by those ids — a dropped
// member's fallback singleton, an advisory-keyed GHSA-only row, a holder
// whose deciding record names the fragment — are reachable only through the
// second and third arms. The three arms UNION (scope_id, generation_id)
// pairs inside one MATERIALIZED candidate set (#10564 H3) instead of ORing
// in one WHERE clause: the OR shape let the planner pick a scope nested loop
// and never touched the advisory and group-members indexes, while
// materialization puts the members arm on its GIN bitmap and deduplicates
// pairs before the heap-heavy join. One join of the pair set against the
// scope's active generation then drops rows left behind by superseded
// generations. The statement reads the per-support table (#11197): the table
// holds live finding facts only, so no tombstone predicate applies. Each arm
// reads its own index (supply_chain_impact_finding_supports_cve_idx and
// _advisory_idx, partial on the non-empty id, and the _members_idx and
// _tokens_idx GINs on the payload), so the partial arms carry the literal
// predicate <> ” that proves the partial index under a generic plan; the
// store drops blank lookup ids, so the predicate never changes an answer. The
// operator suppression scope is a holder like any other: its copies are
// ordinary supports that carry cve_id. Exported for plan-shape proofs:
// EXPLAIN tests concatenate it instead of hand-copying it.
const FindSupplyChainImpactHoldersQuery = `
WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.payload #> '{provenance,advisory_group_members}' ?| $1::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id
`

// recordSupplyChainImpactFanOutStateQuery updates the calling pass's own item,
// fenced exactly like its acknowledgement: the lease owner, a live lease, and the
// attempt it claimed. $1 now, $2 work item id, $3 lease owner, $4 last attempt,
// $5 the new holder list as JSON (NULL with $6 set deletes the list), $6 whether
// to touch the list at all, $7 whether to consume the origin stamp, $8 the stamp
// the pass saw, $9 whether to clear the owed marker, $10 the full-pass marker
// to store (#10972 D2; NULL leaves the stored marker alone). The list is rewritten only when $6 is true, so an empty list is
// never "clear" by accident; a non-empty list also sets the durable replay bit so
// the item's success acknowledgement returns it to pending and the next claim
// drains the list. The stamp is deleted only while it still equals $8, so a newer
// stamp written since the claim survives.
const recordSupplyChainImpactFanOutStateQuery = `
UPDATE fact_work_items
SET payload = ((CASE
                   WHEN NOT $6 THEN payload
                   WHEN $5::jsonb IS NULL THEN payload - '` + supplychaincore.PayloadKeyFanOutRemaining + `'
                   ELSE payload || jsonb_build_object('` + supplychaincore.PayloadKeyFanOutRemaining + `', $5::jsonb)
               END)
               || CASE WHEN $10::jsonb IS NULL THEN '{}'::jsonb
                       ELSE jsonb_build_object('` + supplychaincore.PayloadKeyFullPassMarker + `', $10::jsonb) END)
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
	// Instruments records the supports reads of this store on
	// eshu_dp_supply_chain_impact_supports_reads_total (#11197). Nil records
	// nothing.
	Instruments *telemetry.Instruments
	// ReadTimeout overrides the holder-read statement timeout; zero uses
	// supplyChainImpactFanOutReadTimeout. Tests shorten it.
	ReadTimeout time.Duration
	// FactWriteStop, when set, makes the handler-entry gate also require the finding-fact
	// reject trigger to be enabled (#10880 PR-C2 k): the supports-only writer must not run
	// before the stop that keeps an old writer from writing facts exists. Nil skips the check
	// (tests, and a deployment of a schema without the trigger).
	FactWriteStop *SupplyChainImpactFactWriteStopWatch
}

// RequireSupportsReady is the handler-entry gate of the holder reads
// (supplychaincore.SupplyChainImpactSupportsGate, #11197): one autocommit read of
// the readiness predicate before the pass loads or writes anything. A table that
// is not ready returns the typed non-counting refusal, counted as not_ready on
// eshu_dp_supply_chain_impact_supports_reads_total under the fan-out holder
// reader; a ready table records nothing here, since each holder read counts its
// own served outcome.
func (s SupplyChainImpactFanOutStore) RequireSupportsReady(ctx context.Context) error {
	if s.DB == nil {
		return errors.New("supply chain impact fan-out database is required")
	}
	if err := requireSupplyChainImpactSupportsReady(ctx, s.DB, s.Instruments, telemetry.SupplyChainImpactSupportsReaderFanoutHolders); err != nil {
		return err
	}
	return s.requireFactWriteStopArmed(ctx)
}

// requireFactWriteStopArmed defers the pass, non-counting, while the finding-fact reject trigger is
// not enabled, counted as not_ready under the fact_write_stop reader: a supports-only write before the
// stop exists would let an old writer and a new one disagree about where a finding lives. An
// unreadable trigger fails the gate (counted failed): it is not an enabled one.
func (s SupplyChainImpactFanOutStore) requireFactWriteStopArmed(ctx context.Context) error {
	if s.FactWriteStop == nil {
		return nil
	}
	reader := telemetry.SupplyChainImpactSupportsReaderFactWriteStop
	armed, err := s.FactWriteStop.Armed(ctx)
	if err != nil {
		s.Instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadFailed)
		return err
	}
	if !armed {
		s.Instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadNotReady)
		return &supplychaincore.SupplyChainImpactSupportsNotReadyError{Reader: reader}
	}
	return nil
}

// The store carries the handler-entry gate. The handler finds the gate by an
// optional type assertion, so a signature drift would turn the gate off without an
// error; this pin fails the build on a signature drift. It does not catch a wrapper
// that hides the method.
var _ supplychaincore.SupplyChainImpactSupportsGate = SupplyChainImpactFanOutStore{}

func (s SupplyChainImpactFanOutStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// FindHolderScopes returns the scopes other than ownScopeID that hold an active
// finding row matching any of lookupIDs (CVE, advisory, or member ids),
// sorted. It runs as one read-only transaction under a statement timeout and
// fails with supplychaincore.ErrFanOutReadTimeout when the timeout fires.
func (s SupplyChainImpactFanOutStore) FindHolderScopes(
	ctx context.Context,
	ownScopeID string,
	lookupIDs []string,
) ([]string, error) {
	lookupIDs = nonBlankLookupIDs(lookupIDs)
	if len(lookupIDs) == 0 {
		return nil, nil
	}
	return s.readScopeIDsFromSupports(ctx, telemetry.SupplyChainImpactSupportsReaderFanoutHolders, FindSupplyChainImpactHoldersQuery, lookupIDs, ownScopeID)
}

// nonBlankLookupIDs drops blank lookup ids. The supports table stores the empty
// string for a finding without a CVE or advisory id, and its holder indexes are
// partial on the non-empty id, so a blank id must never reach a statement: it
// would match findings that name no id and could not use the partial index
// (#11197). The fact read compared payload text, which a missing key never
// equalled, so dropping blanks keeps the answer the same.
func nonBlankLookupIDs(lookupIDs []string) []string {
	kept := lookupIDs[:0:0]
	for _, id := range lookupIDs {
		if id != "" {
			kept = append(kept, id)
		}
	}
	return kept
}

// readScopeIDs runs one scope-id query as a short read-only transaction under the
// bounded statement timeout and returns the ids in query order. Every fan-out
// read goes through it, so each has the same timeout, the same typed timeout
// error, and the same retry semantics.
func (s SupplyChainImpactFanOutStore) readScopeIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	return s.readScopeIDsIn(ctx, "", query, args...)
}

// readScopeIDsFromSupports is readScopeIDs for a read that answers from the
// supports table (#11197): the transaction is one repeatable-read snapshot whose
// first statement checks that the table is ready, and a table that is not ready
// fails the read with supplychaincore.SupplyChainImpactSupportsNotReadyError instead of answering
// from a partial table.
func (s SupplyChainImpactFanOutStore) readScopeIDsFromSupports(ctx context.Context, reader, query string, args ...any) ([]string, error) {
	return s.readScopeIDsIn(ctx, reader, query, args...)
}

// readScopeIDsIn runs one scope-id read. A non-empty reader marks a read that
// answers from the supports table: it is counted on the supports reads counter
// and checks readiness first.
func (s SupplyChainImpactFanOutStore) readScopeIDsIn(ctx context.Context, reader, query string, args ...any) ([]string, error) {
	fromSupports := reader != ""
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
	setMode := "SET TRANSACTION READ ONLY"
	if fromSupports {
		setMode = setHolderKeyRangeSnapshotQuery
	}
	if _, err := tx.ExecContext(ctx, setMode); err != nil {
		return nil, fmt.Errorf("set supply chain impact fan-out read read-only: %w", err)
	}
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("set supply chain impact fan-out read timeout: %w", err)
	}
	_ = rows.Close()
	if fromSupports {
		if err := requireSupplyChainImpactSupportsReady(ctx, tx, s.Instruments, reader); err != nil {
			// The typed refusal reaches the queue as it is: wrapping it would
			// replace its non-counting class.
			return nil, err
		}
	}
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
	if fromSupports {
		s.Instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadServed)
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
	var marker any
	if state.FullPassMarker != nil {
		encoded, err := json.Marshal(state.FullPassMarker)
		if err != nil {
			return fmt.Errorf("marshal supply chain impact full-pass marker: %w", err)
		}
		marker = string(encoded)
	}
	result, err := s.DB.ExecContext(ctx, recordSupplyChainImpactFanOutStateQuery,
		s.now(), intent.IntentID, s.LeaseOwner, claimedAtValue(intent), remaining, state.ReplaceRemaining, state.ConsumeOrigin, expected, state.ClearOwed, marker)
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
