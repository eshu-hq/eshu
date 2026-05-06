package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	graphProjectionPhaseStateBatchSize = 250
	graphProjectionPhaseColumnsPerRow  = 8
)

const graphProjectionPhaseStateSchemaSQL = `
CREATE TABLE IF NOT EXISTS graph_projection_phase_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);
CREATE INDEX IF NOT EXISTS graph_projection_phase_state_lookup_idx
    ON graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase);
CREATE INDEX IF NOT EXISTS graph_projection_phase_state_updated_idx
    ON graph_projection_phase_state (updated_at DESC);
`

const upsertGraphProjectionPhaseStateBatchPrefix = `
INSERT INTO graph_projection_phase_state (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at
) VALUES `

const upsertGraphProjectionPhaseStateBatchSuffix = `
ON CONFLICT (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase) DO UPDATE
SET committed_at = EXCLUDED.committed_at,
    updated_at = EXCLUDED.updated_at
`

const lookupGraphProjectionPhaseStateSQL = `
SELECT TRUE
FROM graph_projection_phase_state
WHERE scope_id = $1
  AND acceptance_unit_id = $2
  AND source_run_id = $3
  AND generation_id = $4
  AND keyspace = $5
  AND phase = $6
LIMIT 1
`

// GraphProjectionPhaseStateStore persists graph-write readiness rows in
// PostgreSQL.
type GraphProjectionPhaseStateStore struct {
	db ExecQueryer
}

// NewGraphProjectionPhaseStateStore constructs a store backed by the provided
// database handle.
func NewGraphProjectionPhaseStateStore(db ExecQueryer) *GraphProjectionPhaseStateStore {
	return &GraphProjectionPhaseStateStore{db: db}
}

// GraphProjectionPhaseStateSchemaSQL returns the DDL for graph readiness state.
func GraphProjectionPhaseStateSchemaSQL() string {
	return graphProjectionPhaseStateSchemaSQL
}

// EnsureSchema applies the graph readiness DDL.
func (s *GraphProjectionPhaseStateStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, graphProjectionPhaseStateSchemaSQL)
	return err
}

// Upsert writes readiness rows in batches.
func (s *GraphProjectionPhaseStateStore) Upsert(ctx context.Context, rows []reducer.GraphProjectionPhaseState) error {
	if len(rows) == 0 {
		return nil
	}

	for i := 0; i < len(rows); i += graphProjectionPhaseStateBatchSize {
		end := i + graphProjectionPhaseStateBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := upsertGraphProjectionPhaseStateBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// Lookup reports whether the exact bounded readiness phase exists.
func (s *GraphProjectionPhaseStateStore) Lookup(
	ctx context.Context,
	key reducer.GraphProjectionPhaseKey,
	phase reducer.GraphProjectionPhase,
) (bool, bool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		lookupGraphProjectionPhaseStateSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		key.GenerationID,
		string(key.Keyspace),
		string(phase),
	)
	if err != nil {
		return false, false, fmt.Errorf("query graph projection phase state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, false, nil
	}

	var ready bool
	if err := rows.Scan(&ready); err != nil {
		if err == sql.ErrNoRows {
			return false, false, nil
		}
		return false, false, fmt.Errorf("scan graph projection phase state: %w", err)
	}
	return ready, true, rows.Err()
}

// PublishGraphProjectionPhases implements reducer.GraphProjectionPhasePublisher.
func (s *GraphProjectionPhaseStateStore) PublishGraphProjectionPhases(ctx context.Context, rows []reducer.GraphProjectionPhaseState) error {
	return s.Upsert(ctx, rows)
}

// NewGraphProjectionReadinessLookup performs exact readiness lookup against the
// durable graph projection phase table.
func NewGraphProjectionReadinessLookup(db ExecQueryer) reducer.GraphProjectionReadinessLookup {
	store := NewGraphProjectionPhaseStateStore(db)

	return func(key reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (bool, bool) {
		ready, found, err := store.Lookup(context.Background(), key, phase)
		if err != nil {
			return false, false
		}
		return ready, found
	}
}

// NewGraphProjectionReadinessPrefetch batches exact phase lookups and returns
// an in-memory lookup closure for the current runner cycle.
func NewGraphProjectionReadinessPrefetch(db ExecQueryer) reducer.GraphProjectionReadinessPrefetch {
	store := NewGraphProjectionPhaseStateStore(db)

	return func(ctx context.Context, keys []reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (reducer.GraphProjectionReadinessLookup, error) {
		readyByKey := make(map[string]bool, len(keys))
		for _, key := range keys {
			if err := key.Validate(); err != nil {
				continue
			}
			composite := graphProjectionReadinessCompositeKey(key, phase)
			if _, seen := readyByKey[composite]; seen {
				continue
			}

			ready, found, err := store.Lookup(ctx, key, phase)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			readyByKey[composite] = ready
		}

		return func(key reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (bool, bool) {
			composite := graphProjectionReadinessCompositeKey(key, phase)
			ready, ok := readyByKey[composite]
			return ready, ok
		}, nil
	}
}

func upsertGraphProjectionPhaseStateBatch(ctx context.Context, db ExecQueryer, batch []reducer.GraphProjectionPhaseState) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*graphProjectionPhaseColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		if err := row.Key.Validate(); err != nil {
			return fmt.Errorf("validate graph projection phase key: %w", err)
		}

		committedAt := row.CommittedAt.UTC()
		if committedAt.IsZero() {
			committedAt = time.Now().UTC()
		}
		updatedAt := row.UpdatedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = committedAt
		}

		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * graphProjectionPhaseColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8,
		)
		args = append(args,
			strings.TrimSpace(row.Key.ScopeID),
			strings.TrimSpace(row.Key.AcceptanceUnitID),
			strings.TrimSpace(row.Key.SourceRunID),
			strings.TrimSpace(row.Key.GenerationID),
			strings.TrimSpace(string(row.Key.Keyspace)),
			strings.TrimSpace(string(row.Phase)),
			committedAt,
			updatedAt,
		)
	}

	query := upsertGraphProjectionPhaseStateBatchPrefix + values.String() + upsertGraphProjectionPhaseStateBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert graph projection phase state batch (%d rows): %w", len(batch), err)
	}
	return nil
}

func graphProjectionReadinessCompositeKey(key reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) string {
	return strings.Join([]string{
		strings.TrimSpace(key.ScopeID),
		strings.TrimSpace(key.AcceptanceUnitID),
		strings.TrimSpace(key.SourceRunID),
		strings.TrimSpace(key.GenerationID),
		strings.TrimSpace(string(key.Keyspace)),
		strings.TrimSpace(string(phase)),
	}, "|")
}

func graphProjectionPhaseStateBootstrapDefinition() Definition {
	return Definition{
		Name: "graph_projection_phase_state",
		Path: "schema/data-plane/postgres/012_graph_projection_phase_state.sql",
		SQL:  graphProjectionPhaseStateSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, graphProjectionPhaseStateBootstrapDefinition())
}
