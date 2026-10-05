// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the generation retention event schema DDL and event writer,
// the key-index guard (#7279), countRows, the row-limit selection rules and
// the over-limit batch narrowing (narrowOverLimitBatch, #7127). They share one
// file because this directory is at its dirgate cap
// (scripts/lib/dirgate-grandfather.tsv), which admits no new non-test file.
// Follow-up: extract a generationretention subpackage.

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

// narrowOverLimitBatch gives back the selection's locks for a batch of one
// generation admitted over BatchRowLimit (arbiter ruling arb-7127-3d-c): it
// rolls back to the selection savepoint, re-locks that one scope and
// generation with the targeted query, and recounts under the new lock (the
// counts taken before the rollback were taken under locks that no longer
// exist). Any other batch releases the savepoint unchanged. No row from the
// targeted lock means another session took the candidate: the pass prunes
// nothing and writes no event.
func (s GenerationRetentionStore) narrowOverLimitBatch(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	candidates []generationRetentionCandidate,
	rowCounts map[string]int64,
	eventRowCounts map[string]map[string]int64,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, error) {
	if len(candidates) == 0 {
		return candidates, rowCounts, eventRowCounts, nil
	}
	if len(candidates) > 1 || generationRetentionRowsTotal(rowCounts) <= int64(policy.BatchRowLimit) {
		if _, err := tx.ExecContext(ctx, generationRetentionReleaseSavepointStatement); err != nil {
			return nil, nil, nil, fmt.Errorf("generation retention: release savepoint: %w", err)
		}
		return candidates, rowCounts, eventRowCounts, nil
	}
	if _, err := tx.ExecContext(ctx, generationRetentionRollbackSavepointStatement); err != nil {
		return nil, nil, nil, fmt.Errorf("generation retention: rollback to savepoint: %w", err)
	}
	only := candidates[0]
	if s.beforeTargetedLock != nil {
		s.beforeTargetedLock()
	}
	// The re-lock is timed as candidate selection and the recount as
	// count_rows, so the #7279 phase set stays closed.
	phaseStart := time.Now()
	rows, err := tx.QueryContext(ctx, generationRetentionTargetedCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{}, only.scopeID, only.generationID,
		now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generation retention: lock candidate: %w", err)
	}
	locked := rows.Next()
	if locked {
		err = rows.Scan(&only.scopeID, &only.generationID, &only.scopeKind, &only.supersededAt, &only.observedAt)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generation retention: lock candidate: %w", err)
	}
	if !locked {
		return nil, nil, nil, nil
	}
	phaseStart = time.Now()
	rowCounts, eventRowCounts, _, err = s.countRows(ctx, tx, []string{only.scopeID}, []string{only.generationID})
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, err
	}
	if generationRetentionRowsOutsideLedger(eventRowCounts[only.generationID]) > int64(policy.BatchRowLimit) {
		// Rows outside the ledger only shrink on a recount (#6809); a guard.
		result.Skipped["row_limit"]++
		return nil, nil, nil, nil
	}
	return []generationRetentionCandidate{only}, rowCounts, eventRowCounts, nil
}

// ledgerRetentionTables are the changed-since ledger tables countRows adds to
// the row counts (#7127 ruling 2.8).
var ledgerRetentionTables = []string{
	linksfreshnessstore.TableLinks,
	linksfreshnessstore.TableLinkDeltas,
	linksfreshnessstore.TableLinkBucketCounts,
	linksfreshnessstore.TableActivations,
}

// rowLimitSkipReason names why a skipped candidate with these row counts was
// skipped: "row_limit_ledger" when it exceeds the limit only because of its
// changed-since ledger rows, which defers it to a batch of its own (it leads a
// later batch, admitted alone); "row_limit" otherwise (its rows outside the
// ledger exceed the limit, ADR #2248, or it fits alone and the batch was
// full). Arbiter ruling arb-7127-3d-b.
func rowLimitSkipReason(rows map[string]int64, limit int) string {
	if generationRetentionRowsTotal(rows) > int64(limit) && generationRetentionRowsOutsideLedger(rows) <= int64(limit) {
		return "row_limit_ledger"
	}
	return "row_limit"
}

// generationRetentionRecheckLimit caps the limit re-checks that follow a
// row-limit skip in one candidate query (arbiter ruling arb-7127-3d-b). Each
// re-check is a count run under the batch's scope locks (since #7279, one key
// probe per candidate key). After this many re-checks that still skip, the
// batch keeps its first member only.
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
//  1. rows outside the ledger over the limit: skip (ADR #2248, row_limit);
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
