// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The go-evidence lock (#11327). A pass loads Go reachability evidence from each
// repository's go-reachability scope on a read-only snapshot, then writes its
// findings in a separate transaction. The Go-modules fence retracts a go scope's
// active generation and reopens the scopes that hold a finding for the repository.
// A pass whose first finding for a repository commits after the fence read its
// holders would keep a verdict derived from the retracted generation, and nothing
// would schedule a pass to correct it.
//
// The pass therefore takes a shared transaction-scoped advisory lock keyed by each
// go scope it loaded, as the last statements of its write transaction before the
// commit (apart from its owed marker), and rechecks that every (scope, generation) it
// loaded is still the scope's active generation. The lock is held for the recheck and
// the commit, not the whole write, so a fence waits milliseconds, not seconds. That
// position is safe only while the transaction holds nothing the fence needs before the
// lock: the census guard in storage/postgres pins it, and the owed marker, the one
// statement that updates a fact_work_items row, runs after the lock. The
// fence takes the exclusive form of the same lock before it detaches anything. A
// fence that starts first commits before the pass proceeds, and the recheck then
// aborts the pass; a pass that starts first commits before the fence continues, and
// the fence's holder read then sees it. Neither lock touches a row: the ack, the
// ingestion upsert and the retirement fence never take it.

// SupplyChainImpactGoEvidenceChangedFailureClass classifies a pass that aborted
// before writing because the Go evidence it loaded no longer names its go scope's
// active generation, or because the shared lock timed out behind a fence. It is
// enrolled in nonCountingReducerRetryFailureClasses
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go): another actor's
// legitimate change caused it, and the retry reloads the evidence, so a counting
// class would spend attempts on a loop the pass cannot cause.
const SupplyChainImpactGoEvidenceChangedFailureClass = "supply_chain_impact_go_evidence_changed"

// Causes of a SupplyChainImpactGoEvidenceChangedError. They are the closed values of
// the cause label of eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total.
const (
	// GoEvidenceCauseGenerationChanged: a loaded generation is no longer the go
	// scope's active generation (a fence retracted it, or a successor activated).
	GoEvidenceCauseGenerationChanged = "generation_changed"
	// GoEvidenceCauseLockTimeout: the shared lock hit the lock timeout (SQLSTATE
	// 55P03) behind a fence's exclusive lock.
	GoEvidenceCauseLockTimeout = "lock_timeout"
)

// SupplyChainImpactGoEvidenceChangedError is the typed refusal of a pass whose Go
// evidence went stale before its write. It is retryable and an expected wait, so
// the queue defers the intent after the normal delay without spending an attempt
// and the service records it as deferred.
type SupplyChainImpactGoEvidenceChangedError struct {
	// Cause is one of the GoEvidenceCause values.
	Cause string
	// Err is the database error behind a lock timeout; nil for a changed generation.
	Err error
}

// Error implements error.
func (e *SupplyChainImpactGoEvidenceChangedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("supply chain impact go evidence changed before the write (%s): %v", e.Cause, e.Err)
	}
	return fmt.Sprintf("supply chain impact go evidence changed before the write (%s): reload it", e.Cause)
}

// Unwrap returns the database error, when there is one.
func (e *SupplyChainImpactGoEvidenceChangedError) Unwrap() error { return e.Err }

// Retryable marks the refusal retryable: the retry reloads the evidence.
func (*SupplyChainImpactGoEvidenceChangedError) Retryable() bool { return true }

// ExpectedWait marks the refusal as a designed wait, recorded as deferred.
func (*SupplyChainImpactGoEvidenceChangedError) ExpectedWait() bool { return true }

// FailureClass names the non-counting class for the queue.
func (*SupplyChainImpactGoEvidenceChangedError) FailureClass() string {
	return SupplyChainImpactGoEvidenceChangedFailureClass
}

// GoEvidenceGeneration names one go-reachability scope and the generation the pass's
// loader read Go evidence from. The loader returns exactly the pairs it resolved; a
// scope it asked about but did not resolve is not in the set.
type GoEvidenceGeneration struct {
	ScopeID      string
	GenerationID string
}

// SupplyChainImpactGoEvidenceSharedLockQuery is the shared advisory lock a pass takes
// for one go scope, as the closing statements of its write transaction. The postgres
// transaction wrapper recognises this text to map a lock timeout (55P03) to
// SupplyChainImpactGoEvidenceChangedError.
const SupplyChainImpactGoEvidenceSharedLockQuery = `SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))`

// SupplyChainImpactGoEvidenceExclusiveLockQuery is the exclusive form the Go-modules
// fence takes for the go scope it is about to retract, before any other lock.
const SupplyChainImpactGoEvidenceExclusiveLockQuery = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// SupplyChainImpactGoEvidenceLockKey is the advisory-lock key text for one go scope.
// It is its own derivation: the prefix namespaces it away from the conflict-domain key
// of supplyChainImpactConflictDomainKey (which starts with the finding fact kind) and
// from every other advisory key. A 64-bit hash collision only adds exclusion, never a
// wrong result.
func SupplyChainImpactGoEvidenceLockKey(goScopeID string) string {
	return "supply_chain_impact_go_evidence\x1f" + goScopeID
}

// supplyChainImpactGoEvidenceRecheckQuery returns the loaded pairs that are still
// current: the scope's active pointer names the generation and that generation is
// active. It is a plain SELECT, a new statement after the locks are granted, so
// under READ COMMITTED it sees every fence commit that released the lock. The caller
// compares the row count with the number of pairs it asked about. Every row also
// carries the transaction's isolation level, so a write transaction that is not READ
// COMMITTED (where the recheck would read the snapshot taken before the lock wait and
// silently pass) fails loudly instead of closing nothing.
const supplyChainImpactGoEvidenceRecheckQuery = `
SELECT pair.scope_id, current_setting('transaction_isolation')
FROM unnest($1::text[], $2::text[]) AS pair(scope_id, generation_id)
JOIN ingestion_scopes AS scope
  ON scope.scope_id = pair.scope_id
 AND scope.active_generation_id = pair.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = pair.scope_id
 AND generation.generation_id = pair.generation_id
 AND generation.status = 'active'`

// sortedGoEvidencePairs returns the pairs de-duplicated and sorted by scope id, the
// order the shared locks are taken in.
func sortedGoEvidencePairs(pairs []GoEvidenceGeneration) []GoEvidenceGeneration {
	out := make([]GoEvidenceGeneration, 0, len(pairs))
	seen := make(map[GoEvidenceGeneration]struct{}, len(pairs))
	for _, pair := range pairs {
		if pair.ScopeID == "" || pair.GenerationID == "" {
			continue
		}
		if _, ok := seen[pair]; ok {
			continue
		}
		seen[pair] = struct{}{}
		out = append(out, pair)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScopeID != out[j].ScopeID {
			return out[i].ScopeID < out[j].ScopeID
		}
		return out[i].GenerationID < out[j].GenerationID
	})
	return out
}

// lockAndRecheckGoEvidence runs the closing statements of #11327: one shared lock per
// distinct go scope in sorted order, then one recheck SELECT over every pair. It
// issues neither statement when the pass resolved no pair (locked false). It reports
// the time its lock statements took, whether they were granted or refused. A recheck
// that finds fewer current pairs than the pass loaded returns
// SupplyChainImpactGoEvidenceChangedError.
func lockAndRecheckGoEvidence(ctx context.Context, tx SupplyChainImpactTx, pairs []GoEvidenceGeneration) (locked bool, wait time.Duration, err error) {
	pairs = sortedGoEvidencePairs(pairs)
	if len(pairs) == 0 {
		return false, 0, nil
	}
	lastLocked := ""
	scopeIDs := make([]string, 0, len(pairs))
	generationIDs := make([]string, 0, len(pairs))
	started := time.Now()
	for _, pair := range pairs {
		scopeIDs = append(scopeIDs, pair.ScopeID)
		generationIDs = append(generationIDs, pair.GenerationID)
		if pair.ScopeID == lastLocked {
			continue
		}
		lastLocked = pair.ScopeID
		if _, err := tx.ExecContext(ctx, SupplyChainImpactGoEvidenceSharedLockQuery, SupplyChainImpactGoEvidenceLockKey(pair.ScopeID)); err != nil {
			return true, time.Since(started), fmt.Errorf("lock supply chain impact go evidence: %w", err)
		}
	}
	wait = time.Since(started)
	rows, err := tx.QueryContext(ctx, supplyChainImpactGoEvidenceRecheckQuery, scopeIDs, generationIDs)
	if err != nil {
		return true, wait, fmt.Errorf("recheck supply chain impact go evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	current := 0
	for rows.Next() {
		var scopeID, isolation string
		if err := rows.Scan(&scopeID, &isolation); err != nil {
			return true, wait, fmt.Errorf("scan supply chain impact go evidence recheck: %w", err)
		}
		if isolation != "read committed" {
			return true, wait, fmt.Errorf("supply chain impact go evidence recheck needs a READ COMMITTED write transaction, got %q", isolation)
		}
		current++
	}
	if err := rows.Err(); err != nil {
		return true, wait, fmt.Errorf("recheck supply chain impact go evidence: %w", err)
	}
	if current != len(pairs) {
		return true, wait, &SupplyChainImpactGoEvidenceChangedError{Cause: GoEvidenceCauseGenerationChanged}
	}
	return true, wait, nil
}

// recordOwedAfterGoEvidenceLock records the pass's owed marker, the one statement that
// follows the lock and updates the pass's own fact_work_items row. When the pass holds
// the shared lock the marker's duration, including any wait on that row, is added to
// the lock wait so a fence timeout behind a stalled marker can be explained from the
// lock-wait histogram (#11327). It returns the lock wait including the marker.
func recordOwedAfterGoEvidenceLock(ctx context.Context, tx SupplyChainImpactTx, write SupplyChainImpactWrite, locked bool, wait time.Duration) (time.Duration, error) {
	started := time.Now()
	err := write.Owed.RecordFanOutOwed(ctx, tx)
	if locked {
		wait += time.Since(started)
	}
	if err != nil {
		return wait, fmt.Errorf("record supply chain impact fan-out owed: %w", err)
	}
	return wait, nil
}

// goEvidenceEpilogueViolation reports a statement order that breaks the #11327
// contract, or "" for a clean one. Clean is either no go-evidence statement at all, or
// the shared locks, then the one recheck right after them, then at most the owed-marker
// statement (the pass's own work item, which must follow the lock) and nothing else
// before the commit. It is the checker behind the seeded-violation test, so a rewrite
// of the writer cannot move the lock and the recheck off the end unnoticed.
// markerStatement is the owed-marker query text, or "" when the pass records none.
func goEvidenceEpilogueViolation(statements []string, markerStatement string) string {
	locks, rechecks, markerEarly, queueEarly := 0, 0, false, false
	first := -1
	for i, statement := range statements {
		if first < 0 && strings.Contains(statement, "fact_work_items") {
			queueEarly = true
			markerEarly = markerEarly || (markerStatement != "" && statement == markerStatement)
		}
		switch statement {
		case SupplyChainImpactGoEvidenceSharedLockQuery:
			locks++
			if first < 0 {
				first = i
			}
		case supplyChainImpactGoEvidenceRecheckQuery:
			rechecks++
		}
	}
	if locks == 0 && rechecks == 0 {
		return ""
	}
	if locks == 0 || rechecks != 1 {
		return "the shared locks or the single recheck are missing or repeated"
	}
	if markerEarly {
		return "the owed marker updates the pass's work item before the lock: it can deadlock with a fence"
	}
	if queueEarly {
		return "a statement before the lock names fact_work_items, a row the fence reopens"
	}
	end := first
	for end < len(statements) && statements[end] == SupplyChainImpactGoEvidenceSharedLockQuery {
		end++
	}
	if end-first != locks {
		return "a shared go-evidence lock is not in the closing run of locks"
	}
	if end >= len(statements) || statements[end] != supplyChainImpactGoEvidenceRecheckQuery {
		return "the recheck is not right after the locks"
	}
	rest := statements[end+1:]
	switch {
	case len(rest) == 0:
		return ""
	case len(rest) == 1 && markerStatement != "" && rest[0] == markerStatement:
		return ""
	}
	return "a statement other than the owed marker follows the recheck"
}

// countGoEvidenceRecheckAbort counts a write the go-evidence recheck or its shared
// lock refused, by closed reason (#11327). Any other error counts nothing.
func (h SupplyChainImpactHandler) countGoEvidenceRecheckAbort(ctx context.Context, err error) {
	var changed *SupplyChainImpactGoEvidenceChangedError
	if errors.As(err, &changed) {
		h.Instruments.RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, changed.Cause)
	}
}

// writeTxOutcomeLabel names the outcome of one impact write transaction for the
// duration histogram: committed, recheck_aborted (the go-evidence recheck or its lock
// refused the pass) or rolled_back (any other failure, begin and commit included).
func writeTxOutcomeLabel(err error) string {
	var changed *SupplyChainImpactGoEvidenceChangedError
	switch {
	case err == nil:
		return "committed"
	case errors.As(err, &changed):
		return "recheck_aborted"
	default:
		return "rolled_back"
	}
}
