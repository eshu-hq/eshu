// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

const (
	graphProjectionPhaseStateBatchSize = 250
	graphProjectionPhaseColumnsPerRow  = 8
	// graphProjectionPhaseStateLookupChunkSize bounds one batched
	// readiness lookup (#7724 1A). Each chunk is a single UNNEST/JOIN
	// query over at most this many distinct keys, so a widen round costs
	// ceil(keys/1000) queries instead of one Lookup per key.
	graphProjectionPhaseStateLookupChunkSize = 1000
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

// lookupGraphProjectionPhaseStateBatchSQL resolves one chunk of exact
// bounded readiness keys for one phase in a single round trip (#7724 1A).
// The multi-argument UNNEST zips the five key arrays positionally and the
// JOIN probes the (scope_id, acceptance_unit_id, source_run_id,
// generation_id, keyspace, phase) primary key once per key, so the plan is
// a nested loop over PK index scans with no sequential scan. Only present
// keys return rows; the caller treats absent keys as not-found, exactly
// like a Lookup miss. A present row is always ready: the table stores
// commit markers, and Lookup answers ready=TRUE for every row it finds.
const lookupGraphProjectionPhaseStateBatchSQL = `
SELECT a.scope_id, a.acceptance_unit_id, a.source_run_id, a.generation_id, a.keyspace
FROM UNNEST($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) AS k(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace)
JOIN graph_projection_phase_state AS a
  ON a.scope_id = k.scope_id
 AND a.acceptance_unit_id = k.acceptance_unit_id
 AND a.source_run_id = k.source_run_id
 AND a.generation_id = k.generation_id
 AND a.keyspace = k.keyspace
 AND a.phase = $6
`

// GraphProjectionPhaseStateStore persists graph-write readiness rows in
// PostgreSQL.
type GraphProjectionPhaseStateStore struct {
	database db.ExecQueryer
}

// NewGraphProjectionPhaseStateStore constructs a store backed by the provided
// database handle.
func NewGraphProjectionPhaseStateStore(database db.ExecQueryer) *GraphProjectionPhaseStateStore {
	return &GraphProjectionPhaseStateStore{database: database}
}

// GraphProjectionPhaseStateSchemaSQL returns the DDL for graph readiness state.
func GraphProjectionPhaseStateSchemaSQL() string {
	return graphProjectionPhaseStateSchemaSQL
}

// EnsureSchema applies the graph readiness DDL.
func (s *GraphProjectionPhaseStateStore) EnsureSchema(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, graphProjectionPhaseStateSchemaSQL)
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
		if err := upsertGraphProjectionPhaseStateBatch(ctx, s.database, rows[i:end]); err != nil {
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
	rows, err := s.database.QueryContext(
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
func NewGraphProjectionReadinessLookup(database db.ExecQueryer) reducer.GraphProjectionReadinessLookup {
	store := NewGraphProjectionPhaseStateStore(database)

	return func(key reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (bool, bool) {
		ready, found, err := store.Lookup(context.Background(), key, phase)
		if err != nil {
			return false, false
		}
		return ready, found
	}
}

// LookupBatch returns the subset of keys whose phase row is present in
// graph_projection_phase_state (#7724 1A). It issues one UNNEST/JOIN query
// per chunk of at most graphProjectionPhaseStateLookupChunkSize distinct
// keys, so a widen round costs ceil(keys/1000) queries instead of one
// Lookup per key.
//
// Keys are normalized (TrimSpace) before the query and compared on
// normalized keys, matching the prefetch closure's comparison; keys that
// fail validation cannot match and are skipped without a query. Absent
// keys are simply missing from the result, exactly like a Lookup miss,
// and every returned key is ready (phase rows are publish-once commit
// markers). A query error fails the whole batch: the caller must fail its
// selection rather than drop or keep rows. An empty key set issues no
// query.
//
// The call records keys/queries/rows/duration into the context's
// PrefetchStats when the caller attached one.
func (s *GraphProjectionPhaseStateStore) LookupBatch(
	ctx context.Context,
	keys []reducer.GraphProjectionPhaseKey,
	phase reducer.GraphProjectionPhase,
) ([]reducer.GraphProjectionPhaseKey, error) {
	seen := make(map[string]struct{}, len(keys))
	distinct := make([]reducer.GraphProjectionPhaseKey, 0, len(keys))
	for _, key := range keys {
		key.ScopeID = strings.TrimSpace(key.ScopeID)
		key.AcceptanceUnitID = strings.TrimSpace(key.AcceptanceUnitID)
		key.SourceRunID = strings.TrimSpace(key.SourceRunID)
		key.GenerationID = strings.TrimSpace(key.GenerationID)
		key.Keyspace = reducer.GraphProjectionKeyspace(strings.TrimSpace(string(key.Keyspace)))
		if err := key.Validate(); err != nil {
			continue
		}
		composite := graphProjectionReadinessCompositeKey(key, phase)
		if _, dup := seen[composite]; dup {
			continue
		}
		seen[composite] = struct{}{}
		distinct = append(distinct, key)
	}
	if len(distinct) == 0 {
		return nil, nil
	}

	start := time.Now()
	found := make([]reducer.GraphProjectionPhaseKey, 0, len(distinct))
	queries := 0
	for i := 0; i < len(distinct); i += graphProjectionPhaseStateLookupChunkSize {
		end := i + graphProjectionPhaseStateLookupChunkSize
		if end > len(distinct) {
			end = len(distinct)
		}
		chunkFound, err := s.lookupPhaseChunk(ctx, distinct[i:end], phase)
		if err != nil {
			return nil, err
		}
		found = append(found, chunkFound...)
		queries++
	}
	sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindReadiness, len(distinct), queries, len(found), 0, time.Since(start))
	return found, nil
}

// lookupPhaseChunk resolves one chunk of normalized distinct keys for one
// phase with a single UNNEST/JOIN query and returns the present keys.
func (s *GraphProjectionPhaseStateStore) lookupPhaseChunk(
	ctx context.Context,
	chunk []reducer.GraphProjectionPhaseKey,
	phase reducer.GraphProjectionPhase,
) ([]reducer.GraphProjectionPhaseKey, error) {
	scopes := make([]string, len(chunk))
	units := make([]string, len(chunk))
	runs := make([]string, len(chunk))
	generations := make([]string, len(chunk))
	keyspaces := make([]string, len(chunk))
	for i, key := range chunk {
		scopes[i] = key.ScopeID
		units[i] = key.AcceptanceUnitID
		runs[i] = key.SourceRunID
		generations[i] = key.GenerationID
		keyspaces[i] = string(key.Keyspace)
	}
	rows, err := s.database.QueryContext(
		ctx, lookupGraphProjectionPhaseStateBatchSQL,
		scopes, units, runs, generations, keyspaces, strings.TrimSpace(string(phase)),
	)
	if err != nil {
		return nil, fmt.Errorf("query graph projection phase state batch (%d keys): %w", len(chunk), err)
	}
	defer func() { _ = rows.Close() }()
	var found []reducer.GraphProjectionPhaseKey
	for rows.Next() {
		var scopeID, acceptanceUnitID, sourceRunID, generationID, keyspace string
		if err := rows.Scan(&scopeID, &acceptanceUnitID, &sourceRunID, &generationID, &keyspace); err != nil {
			return nil, fmt.Errorf("scan graph projection phase state batch: %w", err)
		}
		found = append(found, reducer.GraphProjectionPhaseKey{
			ScopeID:          strings.TrimSpace(scopeID),
			AcceptanceUnitID: strings.TrimSpace(acceptanceUnitID),
			SourceRunID:      strings.TrimSpace(sourceRunID),
			GenerationID:     strings.TrimSpace(generationID),
			Keyspace:         reducer.GraphProjectionKeyspace(strings.TrimSpace(keyspace)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph projection phase state batch (%d keys): %w", len(chunk), err)
	}
	return found, nil
}

// NewGraphProjectionReadinessPrefetch batches exact phase lookups and returns
// an in-memory lookup closure for the current runner cycle.
//
// The batch resolves through GraphProjectionPhaseStateStore.LookupBatch:
// one UNNEST/JOIN query per 1000 distinct keys (#7724 1A), replacing the
// former one-Lookup-per-key loop. Per-key found/not-found semantics,
// TrimSpace normalization, and the batch-error-fails-selection contract
// are unchanged.
func NewGraphProjectionReadinessPrefetch(database db.ExecQueryer) reducer.GraphProjectionReadinessPrefetch {
	store := NewGraphProjectionPhaseStateStore(database)

	return func(ctx context.Context, keys []reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (reducer.GraphProjectionReadinessLookup, error) {
		found, err := store.LookupBatch(ctx, keys, phase)
		if err != nil {
			return nil, err
		}
		readyByKey := make(map[string]bool, len(found))
		for _, key := range found {
			readyByKey[graphProjectionReadinessCompositeKey(key, phase)] = true
		}

		return func(key reducer.GraphProjectionPhaseKey, phase reducer.GraphProjectionPhase) (bool, bool) {
			composite := graphProjectionReadinessCompositeKey(key, phase)
			ready, ok := readyByKey[composite]
			return ready, ok
		}, nil
	}
}

func upsertGraphProjectionPhaseStateBatch(ctx context.Context, database db.ExecQueryer, batch []reducer.GraphProjectionPhaseState) error {
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
		args = append(
			args,
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
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
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
