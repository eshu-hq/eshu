// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

// upsertSearchVectorBuildReadyQuery upserts the singleton watermark row
// consumed by go/internal/query's search-vector-ready freshness reader. It is
// written only when SearchVectorBuildRunner.RunOnce completes a bounded sweep
// with zero pending scopes (search caught up), mirroring the
// supply_chain_impact_winners_materialization write in
// supply_chain_impact_canonical_winners_store.go.
const upsertSearchVectorBuildReadyQuery = `
INSERT INTO search_vector_build_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
`

// EshuSearchVectorBuildReadyStore publishes the search_vector_ready
// completion signal for the search-vector build sweep.
type EshuSearchVectorBuildReadyStore struct {
	db Executor
}

// NewEshuSearchVectorBuildReadyStore builds a search-vector-ready signal
// publisher.
func NewEshuSearchVectorBuildReadyStore(db Executor) EshuSearchVectorBuildReadyStore {
	return EshuSearchVectorBuildReadyStore{db: db}
}

// PublishSearchVectorReady upserts the singleton watermark row with the
// current time. Called once per bounded sweep that finds zero pending
// scopes; a rapid, repeated caught-up sweep simply re-stamps the same row, so
// the write is idempotent under retry or repeated invocation.
func (s EshuSearchVectorBuildReadyStore) PublishSearchVectorReady(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("eshu search vector build ready store requires a database")
	}
	if _, err := s.db.ExecContext(ctx, upsertSearchVectorBuildReadyQuery, time.Now().UTC()); err != nil {
		return fmt.Errorf("publish search vector ready watermark: %w", err)
	}
	return nil
}
