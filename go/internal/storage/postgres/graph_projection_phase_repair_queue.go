package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	graphProjectionPhaseRepairQueueBatchSize      = 250
	graphProjectionPhaseRepairQueueColumnsPerRow  = 11
	graphProjectionPhaseRepairQueueDeleteKeyWidth = 6
)

const graphProjectionPhaseRepairQueueSchemaSQL = `
CREATE TABLE IF NOT EXISTS graph_projection_phase_repair_queue (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    enqueued_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);
CREATE INDEX IF NOT EXISTS graph_projection_phase_repair_queue_due_idx
    ON graph_projection_phase_repair_queue (next_attempt_at ASC, enqueued_at ASC);
CREATE INDEX IF NOT EXISTS graph_projection_phase_repair_queue_updated_idx
    ON graph_projection_phase_repair_queue (updated_at DESC);
`

const insertGraphProjectionPhaseRepairQueueBatchPrefix = `
INSERT INTO graph_projection_phase_repair_queue (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, enqueued_at, next_attempt_at,
    attempts, last_error
) VALUES `

const insertGraphProjectionPhaseRepairQueueBatchSuffix = `
ON CONFLICT (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase) DO UPDATE
SET committed_at = EXCLUDED.committed_at,
    enqueued_at = EXCLUDED.enqueued_at,
    next_attempt_at = EXCLUDED.next_attempt_at,
    last_error = EXCLUDED.last_error,
    updated_at = NOW()
`

const listDueGraphProjectionPhaseRepairsSQL = `
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id,
       keyspace, phase, committed_at, enqueued_at, next_attempt_at,
       attempts, last_error
FROM graph_projection_phase_repair_queue
WHERE next_attempt_at <= $1
ORDER BY next_attempt_at ASC, enqueued_at ASC
LIMIT $2
`

const deleteGraphProjectionPhaseRepairsPrefix = `
DELETE FROM graph_projection_phase_repair_queue
WHERE (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase) IN (`

const deleteGraphProjectionPhaseRepairsSuffix = `
)`

const markFailedGraphProjectionPhaseRepairSQL = `
UPDATE graph_projection_phase_repair_queue
SET next_attempt_at = $1,
    last_error = $2,
    updated_at = $3,
    attempts = attempts + 1
WHERE scope_id = $4
  AND acceptance_unit_id = $5
  AND source_run_id = $6
  AND generation_id = $7
  AND keyspace = $8
  AND phase = $9
`

// GraphProjectionPhaseRepairQueueStore persists exact readiness publications
// that must be retried after a durable graph write succeeded.
type GraphProjectionPhaseRepairQueueStore struct {
	db ExecQueryer
}

// NewGraphProjectionPhaseRepairQueueStore constructs a repair queue store.
func NewGraphProjectionPhaseRepairQueueStore(db ExecQueryer) *GraphProjectionPhaseRepairQueueStore {
	return &GraphProjectionPhaseRepairQueueStore{db: db}
}

// GraphProjectionPhaseRepairQueueSchemaSQL returns the DDL for the repair
// queue.
func GraphProjectionPhaseRepairQueueSchemaSQL() string {
	return graphProjectionPhaseRepairQueueSchemaSQL
}

// EnsureSchema applies the repair queue DDL.
func (s *GraphProjectionPhaseRepairQueueStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, graphProjectionPhaseRepairQueueSchemaSQL)
	return err
}

// Enqueue inserts repair rows idempotently in batches.
func (s *GraphProjectionPhaseRepairQueueStore) Enqueue(ctx context.Context, repairs []reducer.GraphProjectionPhaseRepair) error {
	if len(repairs) == 0 {
		return nil
	}

	for i := 0; i < len(repairs); i += graphProjectionPhaseRepairQueueBatchSize {
		end := i + graphProjectionPhaseRepairQueueBatchSize
		if end > len(repairs) {
			end = len(repairs)
		}
		if err := enqueueGraphProjectionPhaseRepairBatch(ctx, s.db, repairs[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// ListDue returns repair rows that are ready to retry at or before now.
func (s *GraphProjectionPhaseRepairQueueStore) ListDue(ctx context.Context, now time.Time, limit int) ([]reducer.GraphProjectionPhaseRepair, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, listDueGraphProjectionPhaseRepairsSQL, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query graph projection phase repair queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	repairs := make([]reducer.GraphProjectionPhaseRepair, 0)
	for rows.Next() {
		var repair reducer.GraphProjectionPhaseRepair
		if err := rows.Scan(
			&repair.Key.ScopeID,
			&repair.Key.AcceptanceUnitID,
			&repair.Key.SourceRunID,
			&repair.Key.GenerationID,
			&repair.Key.Keyspace,
			&repair.Phase,
			&repair.CommittedAt,
			&repair.EnqueuedAt,
			&repair.NextAttemptAt,
			&repair.Attempts,
			&repair.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan graph projection phase repair queue: %w", err)
		}
		repairs = append(repairs, repair)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph projection phase repair queue: %w", err)
	}
	return repairs, nil
}

// Delete removes successfully repaired rows from the queue.
func (s *GraphProjectionPhaseRepairQueueStore) Delete(ctx context.Context, repairs []reducer.GraphProjectionPhaseRepair) error {
	if len(repairs) == 0 {
		return nil
	}

	for i := 0; i < len(repairs); i += graphProjectionPhaseRepairQueueBatchSize {
		end := i + graphProjectionPhaseRepairQueueBatchSize
		if end > len(repairs) {
			end = len(repairs)
		}
		if err := deleteGraphProjectionPhaseRepairBatch(ctx, s.db, repairs[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// MarkFailed records the latest publication error and moves the retry time.
func (s *GraphProjectionPhaseRepairQueueStore) MarkFailed(
	ctx context.Context,
	repair reducer.GraphProjectionPhaseRepair,
	nextAttemptAt time.Time,
	lastError string,
) error {
	if err := repair.Validate(); err != nil {
		return fmt.Errorf("validate graph projection repair: %w", err)
	}

	updatedAt := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		markFailedGraphProjectionPhaseRepairSQL,
		nextAttemptAt.UTC(),
		lastError,
		updatedAt,
		strings.TrimSpace(repair.Key.ScopeID),
		strings.TrimSpace(repair.Key.AcceptanceUnitID),
		strings.TrimSpace(repair.Key.SourceRunID),
		strings.TrimSpace(repair.Key.GenerationID),
		strings.TrimSpace(string(repair.Key.Keyspace)),
		strings.TrimSpace(string(repair.Phase)),
	)
	if err != nil {
		return fmt.Errorf("mark graph projection repair failed: %w", err)
	}
	return nil
}

func enqueueGraphProjectionPhaseRepairBatch(ctx context.Context, db ExecQueryer, batch []reducer.GraphProjectionPhaseRepair) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*graphProjectionPhaseRepairQueueColumnsPerRow)
	var values strings.Builder

	for i, repair := range batch {
		if err := repair.Validate(); err != nil {
			return fmt.Errorf("validate graph projection repair: %w", err)
		}

		committedAt := repair.CommittedAt.UTC()
		if committedAt.IsZero() {
			committedAt = time.Now().UTC()
		}
		enqueuedAt := repair.EnqueuedAt.UTC()
		if enqueuedAt.IsZero() {
			enqueuedAt = committedAt
		}
		nextAttemptAt := repair.NextAttemptAt.UTC()
		if nextAttemptAt.IsZero() {
			nextAttemptAt = enqueuedAt
		}
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * graphProjectionPhaseRepairQueueColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11,
		)
		args = append(args,
			strings.TrimSpace(repair.Key.ScopeID),
			strings.TrimSpace(repair.Key.AcceptanceUnitID),
			strings.TrimSpace(repair.Key.SourceRunID),
			strings.TrimSpace(repair.Key.GenerationID),
			strings.TrimSpace(string(repair.Key.Keyspace)),
			strings.TrimSpace(string(repair.Phase)),
			committedAt,
			enqueuedAt,
			nextAttemptAt,
			repair.Attempts,
			repair.LastError,
		)
	}

	query := insertGraphProjectionPhaseRepairQueueBatchPrefix + values.String() + insertGraphProjectionPhaseRepairQueueBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("enqueue graph projection phase repair batch (%d rows): %w", len(batch), err)
	}
	return nil
}

func deleteGraphProjectionPhaseRepairBatch(ctx context.Context, db ExecQueryer, batch []reducer.GraphProjectionPhaseRepair) error {
	args := make([]any, 0, len(batch)*graphProjectionPhaseRepairQueueDeleteKeyWidth)
	var tuples strings.Builder

	for i, repair := range batch {
		if err := repair.Validate(); err != nil {
			return fmt.Errorf("validate graph projection repair: %w", err)
		}

		if i > 0 {
			tuples.WriteString(", ")
		}
		offset := i * graphProjectionPhaseRepairQueueDeleteKeyWidth
		fmt.Fprintf(
			&tuples,
			"($%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
		)
		args = append(args,
			strings.TrimSpace(repair.Key.ScopeID),
			strings.TrimSpace(repair.Key.AcceptanceUnitID),
			strings.TrimSpace(repair.Key.SourceRunID),
			strings.TrimSpace(repair.Key.GenerationID),
			strings.TrimSpace(string(repair.Key.Keyspace)),
			strings.TrimSpace(string(repair.Phase)),
		)
	}

	query := deleteGraphProjectionPhaseRepairsPrefix + tuples.String() + deleteGraphProjectionPhaseRepairsSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete graph projection phase repair batch (%d rows): %w", len(batch), err)
	}
	return nil
}
