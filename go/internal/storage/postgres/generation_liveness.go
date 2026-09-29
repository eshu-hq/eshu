// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	defaultGenerationActivationDeadline = 30 * time.Minute
	defaultGenerationMaxRecoverAttempts = 5
	defaultGenerationLivenessBatchLimit = 200
	defaultGenerationProgressWindow     = 10 * time.Minute
)

// GenerationLivenessPolicy bounds the liveness sweep that recovers wedged
// active generations. The active generation and any scope with a newer
// generation are never re-driven by the sweep; the projector supersede path
// owns the newer-generation case.
type GenerationLivenessPolicy struct {
	// ActivationDeadline is how long an active generation may make no forward
	// progress past canonical-nodes-committed before it is considered wedged
	// and eligible for recovery.
	ActivationDeadline time.Duration
	// MaxRecoverAttempts bounds the automated re-drive budget per generation so
	// a poison scope cannot loop forever; once exhausted the generation is left
	// for an operator escape hatch.
	MaxRecoverAttempts int
	// BatchLimit caps how many generations a single sweep retires or re-drives.
	BatchLimit int
	// ProgressWindow is how recently a projection_domain queue must have
	// completed any intent for an aged generation with actionable outstanding
	// intent in that domain to count as draining rather than wedged (#7265).
	// Draining generations are skipped and never spend recovery budget.
	ProgressWindow time.Duration
}

// Normalize fills zero or negative fields with the documented defaults so a
// partially configured policy is still safe to run.
func (p GenerationLivenessPolicy) Normalize() GenerationLivenessPolicy {
	if p.ActivationDeadline <= 0 {
		p.ActivationDeadline = defaultGenerationActivationDeadline
	}
	if p.MaxRecoverAttempts <= 0 {
		p.MaxRecoverAttempts = defaultGenerationMaxRecoverAttempts
	}
	if p.BatchLimit <= 0 {
		p.BatchLimit = defaultGenerationLivenessBatchLimit
	}
	if p.ProgressWindow <= 0 {
		p.ProgressWindow = defaultGenerationProgressWindow
	}
	return p
}

// GenerationLivenessResult summarizes one liveness sweep. Superseded counts
// orphaned older active generations retired this cycle; Recovered counts wedged
// active generations re-driven through projector re-enqueue, and Recoveries
// lists each of them in (scope_id, generation_id) order.
type GenerationLivenessResult struct {
	Superseded         int
	Recovered          int
	SupersededScopeIDs []string
	RecoveredScopeIDs  []string
	Recoveries         []GenerationLivenessRecovery
}

// GenerationLivenessRecovery identifies one generation the sweep re-drove and
// the durable liveness_recovery_attempts value the re-enqueue wrote.
type GenerationLivenessRecovery struct {
	ScopeID                  string
	GenerationID             string
	LivenessRecoveryAttempts int
}

// GenerationLivenessStore detects and recovers wedged active generations in
// bounded statements against Postgres. All writes are idempotent under
// concurrent reducer workers and retries; the conflict domain is scope_id.
type GenerationLivenessStore struct {
	database db.ExecQueryer
}

// NewGenerationLivenessStore constructs a Postgres-backed liveness store.
func NewGenerationLivenessStore(database db.ExecQueryer) GenerationLivenessStore {
	return GenerationLivenessStore{database: database}
}

// RecoverWedgedGenerations retires orphaned older active generations and
// re-drives wedged active generations in two bounded statements. It runs the
// supersede pass first so a scope with a newer authoritative generation is
// cleaned up rather than re-driven, then re-enqueues projector work for the
// remaining genuinely-wedged actives. The two statements are independently
// idempotent; they are intentionally not wrapped in one transaction so a slow
// re-enqueue cannot hold a lock across the supersede pass.
func (s GenerationLivenessStore) RecoverWedgedGenerations(
	ctx context.Context,
	policy GenerationLivenessPolicy,
	now time.Time,
) (GenerationLivenessResult, error) {
	if s.database == nil {
		return GenerationLivenessResult{}, errors.New("generation liveness database is required")
	}
	policy = policy.Normalize()
	now = now.UTC()

	supersededScopeIDs, err := s.collectScopeGenerationPairs(
		ctx,
		supersedeOrphanedActiveGenerationsQuery,
		"supersede orphaned active generations",
		now,
		policy.BatchLimit,
	)
	if err != nil {
		return GenerationLivenessResult{}, err
	}

	recoveries, err := s.recoverWedged(ctx, policy, now)
	if err != nil {
		return GenerationLivenessResult{}, err
	}
	recoveredScopeIDs := make([]string, 0, len(recoveries))
	for _, recovery := range recoveries {
		recoveredScopeIDs = append(recoveredScopeIDs, recovery.ScopeID)
	}

	return GenerationLivenessResult{
		Superseded:         len(supersededScopeIDs),
		Recovered:          len(recoveries),
		SupersededScopeIDs: supersededScopeIDs,
		RecoveredScopeIDs:  recoveredScopeIDs,
		Recoveries:         recoveries,
	}, nil
}

// recoverWedged runs the bounded wedged re-drive and returns every re-driven
// generation with the attempt count its re-enqueue wrote.
func (s GenerationLivenessStore) recoverWedged(
	ctx context.Context,
	policy GenerationLivenessPolicy,
	now time.Time,
) ([]GenerationLivenessRecovery, error) {
	const op = "recover wedged active generations"
	rows, err := s.database.QueryContext(
		ctx,
		recoverWedgedActiveGenerationsQuery,
		now.Add(-policy.ActivationDeadline),
		policy.MaxRecoverAttempts,
		policy.BatchLimit,
		now,
		now.Add(-policy.ProgressWindow),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	var recoveries []GenerationLivenessRecovery
	for rows.Next() {
		var recovery GenerationLivenessRecovery
		if scanErr := rows.Scan(&recovery.ScopeID, &recovery.GenerationID, &recovery.LivenessRecoveryAttempts); scanErr != nil {
			return nil, fmt.Errorf("%s: %w", op, scanErr)
		}
		recoveries = append(recoveries, recovery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return recoveries, nil
}

// collectScopeGenerationPairs runs the (scope_id, generation_id)-returning
// supersede query and reports the affected scope ids. The generation id is
// read so the row shape stays explicit; the recovery path uses recoverWedged,
// which surfaces generation ids and attempt counts.
func (s GenerationLivenessStore) collectScopeGenerationPairs(
	ctx context.Context,
	query string,
	op string,
	args ...any,
) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	for rows.Next() {
		var scopeID string
		var generationID string
		if scanErr := rows.Scan(&scopeID, &generationID); scanErr != nil {
			return nil, fmt.Errorf("%s: %w", op, scanErr)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return scopeIDs, nil
}

// CountActiveGenerationsByAge buckets active generations by activation age into
// fresh, aging, draining, and stuck. The stuck bucket uses the same gates the
// recovery sweep uses, so a non-zero stuck count is the operator alarm signal
// that generations are wedging; draining counts blocked generations whose
// domain queues are still progressing inside the progress window.
func (s GenerationLivenessStore) CountActiveGenerationsByAge(
	ctx context.Context,
	policy GenerationLivenessPolicy,
	now time.Time,
) (map[string]int64, error) {
	if s.database == nil {
		return nil, errors.New("generation liveness database is required")
	}
	policy = policy.Normalize()
	now = now.UTC()

	agingBoundary := now.Add(-policy.ActivationDeadline / 2)
	stuckBoundary := now.Add(-policy.ActivationDeadline)

	progressSince := now.Add(-policy.ProgressWindow)

	rows, err := s.database.QueryContext(ctx, countActiveGenerationsByAgeQuery, agingBoundary, stuckBoundary, now, progressSince)
	if err != nil {
		return nil, fmt.Errorf("count active generations by age: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int64{"fresh": 0, "aging": 0, "draining": 0, "stuck": 0}
	for rows.Next() {
		var bucket string
		var count int64
		if scanErr := rows.Scan(&bucket, &count); scanErr != nil {
			return nil, fmt.Errorf("count active generations by age: %w", scanErr)
		}
		counts[bucket] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count active generations by age: %w", err)
	}
	return counts, nil
}
