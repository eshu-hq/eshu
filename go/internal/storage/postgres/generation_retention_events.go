// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the generation retention event schema DDL and event writer,
// the key-index guard (#7279), countRows, the row-limit selection rules, the
// own-fact pre-screen and the selection re-lock (relockRetentionSelection,
// #7334), which replaced narrowOverLimitBatch (#7127) when every pass moved to
// plan-unlocked-then-relock. They share one file because this directory is at
// its dirgate cap (scripts/lib/dirgate-grandfather.tsv), which admits no new
// non-test file. Follow-up: extract a generationretention subpackage.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

const generationRetentionEventSchemaSQL = `
CREATE TABLE IF NOT EXISTS generation_retention_events (
    event_id TEXT PRIMARY KEY,
    scope_id_hash TEXT NOT NULL,
    generation_id_hash TEXT NOT NULL,
    scope_class TEXT NOT NULL,
    policy_scope TEXT NOT NULL,
    policy_revision TEXT NOT NULL,
    generation_observed_at TIMESTAMPTZ NULL,
    generation_superseded_at TIMESTAMPTZ NULL,
    reason TEXT NOT NULL,
    row_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    pruned_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS generation_retention_events_scope_idx
    ON generation_retention_events (scope_id_hash, generation_observed_at DESC);

CREATE INDEX IF NOT EXISTS generation_retention_events_generation_idx
    ON generation_retention_events (scope_id_hash, generation_id_hash);

CREATE INDEX IF NOT EXISTS generation_retention_events_reason_idx
    ON generation_retention_events (reason, pruned_at DESC);
`

const insertGenerationRetentionEventQuery = `
INSERT INTO generation_retention_events (
    event_id,
    scope_id_hash,
    generation_id_hash,
    scope_class,
    policy_scope,
    policy_revision,
    generation_observed_at,
    generation_superseded_at,
    reason,
    row_counts,
    pruned_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11
)
ON CONFLICT (event_id) DO NOTHING
`

// ErrGenerationRetentionKeyIndexUnavailable reports that a retention cycle was
// refused because fact_records_content_entity_key_idx or
// fact_records_file_key_idx (migrations 146 and 147) is missing, invalid, or
// not the expected shape. The retention statements probe those indexes once per
// candidate key; without them each probe scans fact_records while the batch
// holds its scope locks (the #6809 cliff), so the store refuses rather than run
// that shape. The cycle is retried on the runner's next poll.
var ErrGenerationRetentionKeyIndexUnavailable = errors.New("generation retention key index unavailable")

// checkGenerationRetentionKeyIndexes returns an error wrapping
// ErrGenerationRetentionKeyIndexUnavailable that names each unusable key index,
// or nil when both are valid.
func checkGenerationRetentionKeyIndexes(ctx context.Context, tx db.Transaction) error {
	rows, err := tx.QueryContext(ctx, generationRetentionKeyIndexesQuery)
	if err != nil {
		return fmt.Errorf("generation retention: check key indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("generation retention: scan key index check: %w", err)
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("generation retention: check key indexes: %w", err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s missing, invalid, or not the expected shape",
			ErrGenerationRetentionKeyIndexUnavailable, strings.Join(missing, ", "))
	}
	return nil
}

func (s GenerationRetentionStore) recordRetentionEvent(
	ctx context.Context,
	tx db.Transaction,
	candidate generationRetentionCandidate,
	policy GenerationRetentionPolicy,
	rowCounts map[string]int64,
	now time.Time,
) error {
	if rowCounts == nil {
		rowCounts = map[string]int64{}
	}
	rowCountsJSON, err := json.Marshal(rowCounts)
	if err != nil {
		return fmt.Errorf("generation retention: marshal row counts: %w", err)
	}
	scopeHash := retentionHashID("scope", candidate.scopeID)
	generationHash := retentionHashID("generation", candidate.generationID)
	eventID := retentionHashID("event", scopeHash+"\x00"+generationHash+"\x00"+policy.PolicyRevision)
	if _, err := tx.ExecContext(
		ctx,
		insertGenerationRetentionEventQuery,
		eventID,
		scopeHash,
		generationHash,
		candidate.scopeKind,
		policy.PolicyScope,
		policy.PolicyRevision,
		candidate.observedAt,
		candidate.supersededAt,
		"superseded_window_expired",
		rowCountsJSON,
		now,
	); err != nil {
		return fmt.Errorf("generation retention: record event: %w", err)
	}
	return nil
}

func retentionHashID(kind string, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

// countRows runs generationRetentionRowCountsQuery and the changed-since
// ledger's row counts for generationIDs (oldest first, with their scopeIDs in
// parallel) in the retention transaction. It returns the totals per table, the counts per generation and
// table (the events' row_counts), and the grand total BatchRowLimit is
// checked against.
func (s GenerationRetentionStore) countRows(
	ctx context.Context,
	tx db.Transaction,
	scopeIDs []string,
	generationIDs []string,
) (map[string]int64, map[string]map[string]int64, int64, error) {
	rows, err := tx.QueryContext(ctx, generationRetentionRowCountsQuery, generationIDs)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("generation retention: count rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	totals := make(map[string]int64)
	perGeneration := make(map[string]map[string]int64, len(generationIDs))
	var total int64
	for rows.Next() {
		var generationID string
		var tableName string
		var rowCount int64
		if err := rows.Scan(&generationID, &tableName, &rowCount); err != nil {
			return nil, nil, 0, fmt.Errorf("generation retention: scan row count: %w", err)
		}
		if _, ok := perGeneration[generationID]; !ok {
			perGeneration[generationID] = make(map[string]int64)
		}
		perGeneration[generationID][tableName] = rowCount
		totals[tableName] += rowCount
		total += rowCount
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("generation retention: count rows: %w", err)
	}
	// The changed-since link ledger counts too (#7127 ruling 2.8).
	ledger, err := linksfreshnessstore.PrunedGenerationRowCounts(ctx, tx, scopeIDs, generationIDs)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("generation retention: %w", err)
	}
	for generationID, tables := range ledger {
		if _, ok := perGeneration[generationID]; !ok {
			perGeneration[generationID] = make(map[string]int64)
		}
		for tableName, rowCount := range tables {
			perGeneration[generationID][tableName] = rowCount
			totals[tableName] += rowCount
			total += rowCount
		}
	}
	return totals, perGeneration, total, nil
}

// ledgerRetentionTables are the changed-since ledger tables countRows adds to
// the row counts (#7127 ruling 2.8).
var ledgerRetentionTables = []string{
	linksfreshnessstore.TableLinks,
	linksfreshnessstore.TableLinkDeltas,
	linksfreshnessstore.TableLinkBucketCounts,
	linksfreshnessstore.TableActivations,
}

// rowLimitSkipReason names why a counted generation missed the batch:
// row_limit_ledger when only ledger rows push it over (the collector will
// age the link out and a later pass prunes it), row_limit_own_rows when its
// own rows outside the ledger already exceed the limit (no recount can ever
// admit it), row_limit for the deferred rest (a smaller batch or a recount
// may still admit them).
func rowLimitSkipReason(rows map[string]int64, limit int) string {
	rowLimit := int64(limit)
	if generationRetentionRowsTotal(rows) <= rowLimit {
		return "row_limit"
	}
	if generationRetentionRowsOutsideLedger(rows) <= rowLimit {
		return "row_limit_ledger"
	}
	return "row_limit_own_rows"
}

// generationRetentionRecheckLimit caps the limit re-checks that follow a
// row-limit skip in one candidate query (arbiter ruling arb-7127-3d-b). Each
// re-check is a count run; since #7334 the planning re-checks run unlocked
// (the mandatory recount under the re-locked narrow set is separate and
// always runs once). After this many re-checks that still skip, the batch
// keeps its first member only.
const generationRetentionRecheckLimit = 3

// generationRetentionRowsOutsideLedger sums a candidate's rows in the tables
// outside the changed-since ledger: the rows ADR #2248's BatchRowLimit was
// sized for.
func generationRetentionRowsOutsideLedger(rows map[string]int64) int64 {
	var outside int64
	for tableName, count := range rows {
		if !slices.Contains(ledgerRetentionTables, tableName) {
			outside += count
		}
	}
	return outside
}

// selectCandidatesWithinRowLimit picks, oldest first, the candidates one batch
// prunes (arbiter ruling arb-7127-3d-b):
//
//  1. rows outside the ledger over the limit: skip (ADR #2248,
//     row_limit_own_rows);
//  2. nothing selected yet: admit, whatever the candidate's total;
//  3. the batch total plus the candidate over the limit: skip (deferred);
//  4. otherwise admit.
//
// So a batch of two or more stays within BatchRowLimit, a batch of one exceeds
// it by at most its own ledger rows, and every pass that has a candidate
// within the limit outside the ledger prunes at least one generation: a link
// larger than the limit no longer starves the generations it names.
func selectCandidatesWithinRowLimit(
	candidates []generationRetentionCandidate,
	eventRowCounts map[string]map[string]int64,
	limit int,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, []string) {
	var selected []generationRetentionCandidate
	selectedRows := make(map[string]int64)
	selectedEventRows := make(map[string]map[string]int64)
	var skipped []string
	var total int64
	rowLimit := int64(limit)
	for _, candidate := range candidates {
		rows := eventRowCounts[candidate.generationID]
		candidateRows := generationRetentionRowsTotal(rows)
		if generationRetentionRowsOutsideLedger(rows) > rowLimit ||
			(len(selected) > 0 && total+candidateRows > rowLimit) {
			skipped = append(skipped, candidate.generationID)
			continue
		}
		selected = append(selected, candidate)
		selectedEventRows[candidate.generationID] = rows
		for tableName, count := range rows {
			selectedRows[tableName] += count
		}
		total += candidateRows
	}
	return selected, selectedRows, selectedEventRows, skipped
}

// recheckRetentionBatch settles the provisionally selected set from the first
// count: it fits the batch, recounts the survivors while anything is skipped,
// and re-fits, at most generationRetentionRecheckLimit times, then keeps the
// first member only (arbiter ruling arb-7127-3d-b). Since #7334 these
// re-checks run unlocked; the caller's re-lock recounts once more under the
// narrow lock set.
func (s GenerationRetentionStore) recheckRetentionBatch(
	ctx context.Context,
	tx db.Transaction,
	countable []generationRetentionCandidate,
	eventRowCounts map[string]map[string]int64,
	limit int,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, error) {
	selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
		countable, eventRowCounts, limit)
	for rechecks := 0; len(skipped) > 0; rechecks++ {
		for _, generationID := range skipped {
			result.Skipped[rowLimitSkipReason(eventRowCounts[generationID], limit)]++
		}
		if len(selected) == 0 {
			break
		}
		if rechecks == generationRetentionRecheckLimit {
			// Keep the first member only; its rows outside the ledger only
			// shrink on a recount, so one recount settles it.
			for _, dropped := range selected[1:] {
				result.Skipped[rowLimitSkipReason(eventRowCounts[dropped.generationID], limit)]++
			}
			selected = selected[:1]
		}
		// Skipped generations stay on disk. Their facts protect keys the
		// count charged to a selected one (#6809), and a changed-since link
		// shared with one is still deleted with the selected generation, so
		// its charge moves there (#7127). The recount can shrink or grow;
		// re-check the limit, at most generationRetentionRecheckLimit times.
		phaseStart := time.Now()
		_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(selected), retentionGenerationIDs(selected))
		result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
		if err != nil {
			return nil, nil, nil, err
		}
		selected, rowCounts, selectedEventRowCounts, skipped = selectCandidatesWithinRowLimit(
			selected, eventRowCounts, limit)
		if rechecks == generationRetentionRecheckLimit {
			break
		}
	}
	return selected, rowCounts, selectedEventRowCounts, nil
}

func retentionScopeIDs(candidates []generationRetentionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.scopeID)
	}
	return ids
}

func oldestEligibleAge(now time.Time, candidates []generationRetentionCandidate) time.Duration {
	var oldest time.Duration
	for _, candidate := range candidates {
		age := now.Sub(candidate.supersededAt)
		if age > oldest {
			oldest = age
		}
	}
	return oldest
}

func generationRetentionRowsTotal(rows map[string]int64) int64 {
	var total int64
	for _, count := range rows {
		total += count
	}
	return total
}

// scanRetentionCandidates scans candidate rows from a query whose column
// order matches the selection queries: scope id, generation id, scope kind,
// superseded at, observed at. The caller wraps the error with its query
// context.
func scanRetentionCandidates(rows db.Rows) ([]generationRetentionCandidate, error) {
	var candidates []generationRetentionCandidate
	for rows.Next() {
		var candidate generationRetentionCandidate
		if err := rows.Scan(&candidate.scopeID, &candidate.generationID, &candidate.scopeKind, &candidate.supersededAt, &candidate.observedAt, &candidate.uncovered); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("generation retention: scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	return candidates, nil
}

// sortRetentionCandidatesOldestFirst orders candidates so the batch fitting
// admits the oldest generation first; the re-lock's loose rows need the same
// order after the targeted query returns them in lock order.
func sortRetentionCandidatesOldestFirst(candidates []generationRetentionCandidate) {
	slices.SortFunc(candidates, func(a, b generationRetentionCandidate) int {
		if c := a.supersededAt.Compare(b.supersededAt); c != 0 {
			return c
		}
		return strings.Compare(a.generationID, b.generationID)
	})
}

// prescreenOwnFacts runs the own-fact pre-screen (#7334 fix 2): one grouped,
// unlocked count over fact_records before the heavyweight row count.
// Generations missing from the result count as zero.
func (s GenerationRetentionStore) prescreenOwnFacts(
	ctx context.Context,
	tx db.Transaction,
	candidates []generationRetentionCandidate,
) (map[string]int64, error) {
	ownFacts := make(map[string]int64, len(candidates))
	for _, candidate := range candidates {
		ownFacts[candidate.generationID] = 0
	}
	rows, err := tx.QueryContext(ctx, generationRetentionPrescreenQuery, retentionGenerationIDs(candidates))
	if err != nil {
		return nil, fmt.Errorf("generation retention: pre-screen own facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var generationID string
		var count int64
		if err := rows.Scan(&generationID, &count); err != nil {
			return nil, fmt.Errorf("generation retention: scan pre-screen: %w", err)
		}
		ownFacts[generationID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("generation retention: pre-screen own facts: %w", err)
	}
	return ownFacts, nil
}

// relockRetentionSelection re-locks the provisionally selected set after the
// selection's savepoint rollback (#7334 fix 1) and recounts it once under
// the new locks; the recount's counts replace the unlocked planning counts
// for the prune decision and the pruning events. The targeted query holds
// only the selected rows (scope, generation) in deterministic order, so two
// overlapping passes lock the same rows in the same sequence. A candidate
// the re-lock misses was taken, re-activated or given work after the
// rollback: the pass silently drops it and prunes the locked subset. held
// reports whether the re-lock locked at least one row, so the caller can
// scope its lock-hold measurement to the narrow window.
func (s GenerationRetentionStore) relockRetentionSelection(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	candidates []generationRetentionCandidate,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, bool, error) {
	if len(candidates) == 0 {
		return nil, nil, nil, false, nil
	}
	if s.beforeTargetedLock != nil {
		s.beforeTargetedLock()
	}
	phaseStart := time.Now()
	rows, err := tx.QueryContext(ctx, generationRetentionTargetedCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{},
		retentionScopeIDs(candidates), retentionGenerationIDs(candidates),
		now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("generation retention: lock candidates: %w", err)
	}
	locked, err := scanRetentionCandidates(rows)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("generation retention: lock candidates: %w", err)
	}
	if len(locked) == 0 {
		return nil, nil, nil, false, nil
	}
	// The unlocked planning counts were taken under locks that no longer
	// exist; the recount under the new locks is the authoritative one. One
	// recount settles the batch: it counts exactly the locked set, so key
	// attribution already reflects any member the re-lock dropped.
	sortRetentionCandidatesOldestFirst(locked)
	phaseStart = time.Now()
	_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(locked), retentionGenerationIDs(locked))
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, false, err
	}
	selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
		locked, eventRowCounts, policy.BatchRowLimit)
	for _, generationID := range skipped {
		result.Skipped[rowLimitSkipReason(eventRowCounts[generationID], policy.BatchRowLimit)]++
	}
	if len(selected) == 0 {
		return nil, nil, nil, true, nil
	}
	return selected, rowCounts, selectedEventRowCounts, true, nil
}
