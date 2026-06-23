package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultGenerationActivationDeadline = 30 * time.Minute
	defaultGenerationMaxRecoverAttempts = 5
	defaultGenerationLivenessBatchLimit = 200
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
	return p
}

// GenerationLivenessResult summarizes one liveness sweep. Superseded counts
// orphaned older active generations retired this cycle; Recovered counts wedged
// active generations re-driven through projector re-enqueue.
type GenerationLivenessResult struct {
	Superseded         int
	Recovered          int
	SupersededScopeIDs []string
	RecoveredScopeIDs  []string
}

// GenerationLivenessStore detects and recovers wedged active generations in
// bounded statements against Postgres. All writes are idempotent under
// concurrent reducer workers and retries; the conflict domain is scope_id.
type GenerationLivenessStore struct {
	db ExecQueryer
}

// NewGenerationLivenessStore constructs a Postgres-backed liveness store.
func NewGenerationLivenessStore(db ExecQueryer) GenerationLivenessStore {
	return GenerationLivenessStore{db: db}
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
	if s.db == nil {
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

	deadline := now.Add(-policy.ActivationDeadline)
	recoveredScopeIDs, err := s.collectScopeGenerationPairs(
		ctx,
		recoverWedgedActiveGenerationsQuery,
		"recover wedged active generations",
		deadline,
		policy.MaxRecoverAttempts,
		policy.BatchLimit,
		now,
	)
	if err != nil {
		return GenerationLivenessResult{}, err
	}

	return GenerationLivenessResult{
		Superseded:         len(supersededScopeIDs),
		Recovered:          len(recoveredScopeIDs),
		SupersededScopeIDs: supersededScopeIDs,
		RecoveredScopeIDs:  recoveredScopeIDs,
	}, nil
}

// collectScopeGenerationPairs runs a (scope_id, generation_id)-returning query
// and reports the affected scope ids. The generation id is read so the row
// shape stays explicit for callers and future telemetry, but only scope ids are
// surfaced today.
func (s GenerationLivenessStore) collectScopeGenerationPairs(
	ctx context.Context,
	query string,
	op string,
	args ...any,
) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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
// fresh, aging, and stuck. The stuck bucket uses the same activation deadline
// the recovery sweep uses, so a non-zero stuck count is the operator alarm
// signal that generations are wedging.
func (s GenerationLivenessStore) CountActiveGenerationsByAge(
	ctx context.Context,
	policy GenerationLivenessPolicy,
	now time.Time,
) (map[string]int64, error) {
	if s.db == nil {
		return nil, errors.New("generation liveness database is required")
	}
	policy = policy.Normalize()
	now = now.UTC()

	agingBoundary := now.Add(-policy.ActivationDeadline / 2)
	stuckBoundary := now.Add(-policy.ActivationDeadline)

	rows, err := s.db.QueryContext(ctx, countActiveGenerationsByAgeQuery, agingBoundary, stuckBoundary)
	if err != nil {
		return nil, fmt.Errorf("count active generations by age: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int64{"fresh": 0, "aging": 0, "stuck": 0}
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
