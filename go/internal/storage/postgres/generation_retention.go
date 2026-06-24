// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	defaultGenerationRetentionMinSuperseded = 24
	defaultGenerationRetentionMaxAge        = 7 * 24 * time.Hour
	defaultGenerationRetentionBatchLimit    = 100
	defaultGenerationRetentionRowLimit      = 100000
	defaultGenerationRetentionPolicyScope   = "global"
	defaultGenerationRetentionPolicyRev     = "global-default-v1"
)

// GenerationRetentionPolicy bounds automated cleanup of superseded source-local
// generations. The active generation and the newest superseded generations
// inside the count or age window are never candidates.
type GenerationRetentionPolicy struct {
	MinSupersededGenerations int
	MaxSupersededAge         time.Duration
	BatchGenerationLimit     int
	BatchRowLimit            int
	PolicyScope              string
	PolicyRevision           string
}

// DefaultGenerationRetentionPolicy returns the ADR #2248 default retention
// window: keep the active generation plus the last 24 superseded generations or
// any superseded generation newer than seven days, whichever keeps more.
func DefaultGenerationRetentionPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: defaultGenerationRetentionMinSuperseded,
		MaxSupersededAge:         defaultGenerationRetentionMaxAge,
		BatchGenerationLimit:     defaultGenerationRetentionBatchLimit,
		BatchRowLimit:            defaultGenerationRetentionRowLimit,
		PolicyScope:              defaultGenerationRetentionPolicyScope,
		PolicyRevision:           defaultGenerationRetentionPolicyRev,
	}
}

func (p GenerationRetentionPolicy) normalize() GenerationRetentionPolicy {
	defaults := DefaultGenerationRetentionPolicy()
	if p.MinSupersededGenerations < 0 {
		p.MinSupersededGenerations = defaults.MinSupersededGenerations
	}
	if p.MaxSupersededAge <= 0 {
		p.MaxSupersededAge = defaults.MaxSupersededAge
	}
	if p.BatchGenerationLimit <= 0 {
		p.BatchGenerationLimit = defaults.BatchGenerationLimit
	}
	if p.BatchRowLimit <= 0 {
		p.BatchRowLimit = defaults.BatchRowLimit
	}
	if p.PolicyScope == "" {
		p.PolicyScope = defaults.PolicyScope
	}
	if p.PolicyRevision == "" {
		p.PolicyRevision = defaults.PolicyRevision
	}
	return p
}

// GenerationRetentionResult reports the work a cleanup transaction completed.
// RowsPruned is keyed by bounded table/data-class names, never raw scope or
// generation identifiers.
type GenerationRetentionResult struct {
	GenerationsPruned int
	RowsPruned        map[string]int64
	Skipped           map[string]int
	OldestEligibleAge time.Duration
	Duration          time.Duration
}

// GenerationRetentionStore prunes superseded source-local generation history in
// bounded transactions while preserving active reads and changed-since truth.
type GenerationRetentionStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewGenerationRetentionStore constructs a Postgres-backed retention cleanup
// store. The supplied database must support transactions when cleanup runs.
func NewGenerationRetentionStore(db ExecQueryer) GenerationRetentionStore {
	return GenerationRetentionStore{db: db}
}

// PruneSupersededGenerations deletes one bounded batch of superseded
// generations outside the retained window. It records safe retention events
// before deletion and removes generation-owned rows through FK cascades.
func (s GenerationRetentionStore) PruneSupersededGenerations(
	ctx context.Context,
	policy GenerationRetentionPolicy,
) (GenerationRetentionResult, error) {
	if s.db == nil {
		return GenerationRetentionResult{}, errors.New("generation retention database is required")
	}
	beginner, ok := s.db.(Beginner)
	if !ok {
		return GenerationRetentionResult{}, errors.New("generation retention database must support Begin")
	}

	policy = policy.normalize()
	start := time.Now()
	now := s.now()
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result := GenerationRetentionResult{
		RowsPruned: make(map[string]int64),
		Skipped:    make(map[string]int),
	}
	candidates, rowCounts, eventRowCounts, err := s.selectPrunableCandidates(ctx, tx, now, policy, &result)
	if err != nil {
		return GenerationRetentionResult{}, err
	}
	if len(candidates) == 0 {
		if err := tx.Commit(); err != nil {
			return GenerationRetentionResult{}, fmt.Errorf("generation retention: commit empty batch: %w", err)
		}
		committed = true
		result.Duration = time.Since(start)
		return result, nil
	}
	result.OldestEligibleAge = oldestEligibleAge(now, candidates)

	generationIDs := retentionGenerationIDs(candidates)
	for tableName, count := range rowCounts {
		result.RowsPruned[tableName] = count
	}

	for _, candidate := range candidates {
		if err := s.recordRetentionEvent(ctx, tx, candidate, policy, eventRowCounts[candidate.generationID], now); err != nil {
			return GenerationRetentionResult{}, err
		}
	}
	if affected, err := execRowsAffected(ctx, tx, deleteSharedProjectionIntentsForGenerationsQuery, generationIDs); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: delete shared projection intents: %w", err)
	} else {
		result.RowsPruned["shared_projection_intents"] = affected
	}
	if affected, err := execRowsAffected(ctx, tx, pruneContentFileReferencesForGenerationsQuery, generationIDs); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: prune content references: %w", err)
	} else {
		result.RowsPruned["content_file_references"] = affected
	}
	if affected, err := execRowsAffected(ctx, tx, pruneContentEntitiesForGenerationsQuery, generationIDs); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: prune content entities: %w", err)
	} else {
		result.RowsPruned["content_entities"] = affected
	}
	if affected, err := execRowsAffected(ctx, tx, pruneContentFilesForGenerationsQuery, generationIDs); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: prune content files: %w", err)
	} else {
		result.RowsPruned["content_files"] = affected
	}
	generationsPruned, err := execRowsAffected(ctx, tx, deleteScopeGenerationsForRetentionQuery, generationIDs)
	if err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: delete scope generations: %w", err)
	}
	result.GenerationsPruned = int(generationsPruned)
	result.RowsPruned["scope_generations"] = generationsPruned

	if err := tx.Commit(); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: commit: %w", err)
	}
	committed = true
	result.Duration = time.Since(start)
	return result, nil
}

func (s GenerationRetentionStore) selectPrunableCandidates(
	ctx context.Context,
	tx Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, error) {
	excludedGenerationIDs := make([]string, 0)
	searchLimit := generationRetentionSkipSearchLimit(policy.BatchGenerationLimit)
	for {
		candidates, err := s.selectCandidates(ctx, tx, now, policy, excludedGenerationIDs)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(candidates) == 0 {
			return nil, nil, nil, nil
		}

		generationIDs := retentionGenerationIDs(candidates)
		_, eventRowCounts, _, err := s.countRows(ctx, tx, generationIDs)
		if err != nil {
			return nil, nil, nil, err
		}
		selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
			candidates,
			eventRowCounts,
			policy.BatchRowLimit,
		)
		if len(skipped) > 0 {
			result.Skipped["row_limit"] += len(skipped)
			excludedGenerationIDs = append(excludedGenerationIDs, skipped...)
		}
		if len(selected) > 0 {
			return selected, rowCounts, selectedEventRowCounts, nil
		}
		if len(excludedGenerationIDs) >= searchLimit {
			return nil, nil, nil, nil
		}
	}
}

type generationRetentionCandidate struct {
	scopeID      string
	generationID string
	scopeKind    string
	supersededAt time.Time
	observedAt   time.Time
}

func (s GenerationRetentionStore) selectCandidates(
	ctx context.Context,
	tx Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	excludedGenerationIDs []string,
) ([]generationRetentionCandidate, error) {
	rows, err := tx.QueryContext(
		ctx,
		generationRetentionCandidateQuery,
		now.Add(-policy.MaxSupersededAge),
		policy.MinSupersededGenerations,
		policy.BatchGenerationLimit,
		excludedGenerationIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []generationRetentionCandidate
	for rows.Next() {
		var candidate generationRetentionCandidate
		if err := rows.Scan(
			&candidate.scopeID,
			&candidate.generationID,
			&candidate.scopeKind,
			&candidate.supersededAt,
			&candidate.observedAt,
		); err != nil {
			return nil, fmt.Errorf("generation retention: scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	return candidates, nil
}

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
		if candidateRows > rowLimit || total+candidateRows > rowLimit {
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

func (s GenerationRetentionStore) countRows(
	ctx context.Context,
	tx Transaction,
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
	return totals, perGeneration, total, nil
}

func (s GenerationRetentionStore) recordRetentionEvent(
	ctx context.Context,
	tx Transaction,
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

func (s GenerationRetentionStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func retentionGenerationIDs(candidates []generationRetentionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.generationID)
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

func generationRetentionSkipSearchLimit(batchLimit int) int {
	limit := batchLimit * 4
	if limit < 16 {
		return 16
	}
	return limit
}

func execRowsAffected(ctx context.Context, exec Executor, query string, args ...any) (int64, error) {
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func retentionHashID(kind string, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}
