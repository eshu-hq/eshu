// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

const (
	defaultGenerationDeadLetterReplayLimit = 100
	maxGenerationDeadLetterFailureMessage  = 4096
)

const insertCollectorGenerationDeadLetterSQL = `
INSERT INTO collector_generation_dead_letters (
    last_dead_lettered_at,
    scope_id,
    generation_id,
    collector_kind,
    source_system,
    scope_kind,
    partition_key,
    trigger_kind,
    payload_reference,
    failure_class,
    failure_message,
    status,
    replay_count,
    first_dead_lettered_at,
    updated_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9::jsonb,
    $10,
    $11,
    'dead_letter',
    0,
    $1,
    $1
)
ON CONFLICT (generation_id) DO UPDATE
SET scope_id = EXCLUDED.scope_id,
    collector_kind = EXCLUDED.collector_kind,
    source_system = EXCLUDED.source_system,
    scope_kind = EXCLUDED.scope_kind,
    partition_key = EXCLUDED.partition_key,
    trigger_kind = EXCLUDED.trigger_kind,
    payload_reference = EXCLUDED.payload_reference,
    failure_class = EXCLUDED.failure_class,
    failure_message = EXCLUDED.failure_message,
    status = 'dead_letter',
    last_dead_lettered_at = EXCLUDED.last_dead_lettered_at,
    updated_at = EXCLUDED.updated_at
`

// CollectorGenerationDeadLetterStore persists and replays collector generation
// commit failures that happened outside the normal scope-generation transaction.
type CollectorGenerationDeadLetterStore struct {
	db ExecQueryer
}

var (
	_ collector.GenerationDeadLetterSink            = CollectorGenerationDeadLetterStore{}
	_ collector.GenerationDeadLetterReplayCompleter = CollectorGenerationDeadLetterStore{}
)

// NewCollectorGenerationDeadLetterStore constructs a Postgres-backed collector
// generation dead-letter store.
func NewCollectorGenerationDeadLetterStore(db ExecQueryer) CollectorGenerationDeadLetterStore {
	return CollectorGenerationDeadLetterStore{db: db}
}

// RecordGenerationDeadLetter stores one failed collector generation idempotently
// so later operator replay can target the exact generation boundary.
func (s CollectorGenerationDeadLetterStore) RecordGenerationDeadLetter(
	ctx context.Context,
	record collector.GenerationDeadLetter,
) error {
	if s.db == nil {
		return fmt.Errorf("collector generation dead-letter store database is required")
	}
	if err := record.Generation.ValidateForScope(record.Scope); err != nil {
		return fmt.Errorf("validate collector generation dead-letter: %w", err)
	}
	failureClass := strings.TrimSpace(record.FailureClass)
	if failureClass == "" {
		return fmt.Errorf("collector generation dead-letter failure class is required")
	}
	deadLetteredAt := record.DeadLetteredAt.UTC()
	if record.DeadLetteredAt.IsZero() {
		deadLetteredAt = time.Now().UTC()
	}
	payloadReference := record.PayloadReference
	if len(payloadReference) == 0 {
		payloadReference = map[string]string{
			"collector_kind": string(record.Scope.CollectorKind),
			"generation_id":  record.Generation.GenerationID,
			"partition_key":  record.Scope.PartitionKey,
			"scope_id":       record.Scope.ScopeID,
			"scope_kind":     string(record.Scope.ScopeKind),
			"source_system":  record.Scope.SourceSystem,
			"trigger_kind":   string(record.Generation.TriggerKind),
		}
	}
	payload, err := marshalPayload(stringMapToAny(payloadReference))
	if err != nil {
		return fmt.Errorf("marshal collector generation dead-letter payload: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		insertCollectorGenerationDeadLetterSQL,
		deadLetteredAt,
		record.Scope.ScopeID,
		record.Generation.GenerationID,
		string(record.Scope.CollectorKind),
		record.Scope.SourceSystem,
		string(record.Scope.ScopeKind),
		record.Scope.PartitionKey,
		string(record.Generation.TriggerKind),
		payload,
		failureClass,
		boundedGenerationDeadLetterFailureMessage(record.FailureMessage),
	); err != nil {
		return fmt.Errorf("record collector generation dead-letter: %w", err)
	}

	return nil
}

// ReplayGenerationDeadLetters marks matching dead-lettered generations as
// replay-requested for an operator or coordinator handoff.
func (s CollectorGenerationDeadLetterStore) ReplayGenerationDeadLetters(
	ctx context.Context,
	filter collector.GenerationDeadLetterReplayFilter,
	now time.Time,
) (collector.GenerationDeadLetterReplayResult, error) {
	if s.db == nil {
		return collector.GenerationDeadLetterReplayResult{}, fmt.Errorf("collector generation dead-letter store database is required")
	}
	replayAt := now.UTC()
	if now.IsZero() {
		replayAt = time.Now().UTC()
	}
	query, args := buildReplayCollectorGenerationDeadLettersQuery(filter, replayAt)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return collector.GenerationDeadLetterReplayResult{}, fmt.Errorf("replay collector generation dead-letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	generationIDs := make([]string, 0)
	for rows.Next() {
		var generationID string
		if scanErr := rows.Scan(&generationID); scanErr != nil {
			return collector.GenerationDeadLetterReplayResult{}, fmt.Errorf("replay collector generation dead-letters: %w", scanErr)
		}
		generationIDs = append(generationIDs, generationID)
	}
	if err := rows.Err(); err != nil {
		return collector.GenerationDeadLetterReplayResult{}, fmt.Errorf("replay collector generation dead-letters: %w", err)
	}

	return collector.GenerationDeadLetterReplayResult{
		Replayed:      len(generationIDs),
		GenerationIDs: generationIDs,
	}, nil
}

// CompleteGenerationDeadLetterReplay clears unresolved dead-letter rows for a
// source scope once the collector has committed a later generation for that
// scope.
func (s CollectorGenerationDeadLetterStore) CompleteGenerationDeadLetterReplay(
	ctx context.Context,
	completion collector.GenerationDeadLetterReplayCompletion,
) error {
	if s.db == nil {
		return fmt.Errorf("collector generation dead-letter store database is required")
	}
	if err := completion.Generation.ValidateForScope(completion.Scope); err != nil {
		return fmt.Errorf("validate collector generation replay completion: %w", err)
	}
	completedAt := completion.CompletedAt.UTC()
	if completion.CompletedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		completeCollectorGenerationDeadLetterReplaySQL,
		completedAt,
		completion.Scope.ScopeID,
		string(completion.Scope.CollectorKind),
		completion.Scope.SourceSystem,
		completion.Scope.PartitionKey,
		completion.Generation.GenerationID,
	)
	if err != nil {
		return fmt.Errorf("complete collector generation dead-letter replay: %w", err)
	}
	return nil
}

func buildReplayCollectorGenerationDeadLettersQuery(
	filter collector.GenerationDeadLetterReplayFilter,
	replayAt time.Time,
) (string, []any) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultGenerationDeadLetterReplayLimit
	}
	args := []any{replayAt, limit}
	conditions := []string{"status = 'dead_letter'"}

	if len(filter.ScopeIDs) > 0 {
		args = append(args, filter.ScopeIDs)
		conditions = append(conditions, fmt.Sprintf("scope_id = ANY($%d)", len(args)))
	}
	if failureClass := strings.TrimSpace(filter.FailureClass); failureClass != "" {
		args = append(args, failureClass)
		conditions = append(conditions, fmt.Sprintf("failure_class = $%d", len(args)))
	}
	if filter.CollectorKind != "" {
		args = append(args, string(filter.CollectorKind))
		conditions = append(conditions, fmt.Sprintf("collector_kind = $%d", len(args)))
	}

	query := fmt.Sprintf(`
WITH candidates AS (
    SELECT generation_id
    FROM collector_generation_dead_letters
    WHERE %s
    ORDER BY last_dead_lettered_at ASC, generation_id ASC
    LIMIT $2
),
replayed AS (
    UPDATE collector_generation_dead_letters
    SET status = 'replay_requested',
        replay_count = replay_count + 1,
        last_replay_requested_at = $1,
        updated_at = $1
    WHERE generation_id IN (SELECT generation_id FROM candidates)
    RETURNING generation_id
)
SELECT generation_id FROM replayed ORDER BY generation_id
`, strings.Join(conditions, "\n      AND "))

	return query, args
}

const completeCollectorGenerationDeadLetterReplaySQL = `
UPDATE collector_generation_dead_letters
SET status = 'replayed',
    last_replayed_at = $1,
    replayed_generation_id = $6,
    updated_at = $1
WHERE scope_id = $2
  AND collector_kind = $3
  AND source_system = $4
  AND partition_key = $5
  AND status IN ('dead_letter', 'replay_requested')
`

func boundedGenerationDeadLetterFailureMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= maxGenerationDeadLetterFailureMessage {
		return message
	}
	return message[:maxGenerationDeadLetterFailureMessage]
}
