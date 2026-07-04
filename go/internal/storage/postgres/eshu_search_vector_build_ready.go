// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

// upsertSearchVectorBuildReadyQuery upserts the vector-identity-keyed
// watermark row consumed by go/internal/query's search-vector-ready
// freshness reader. It is written only when
// SearchVectorBuildRunner.RunOnce's post-build re-check finds zero pending
// scopes for the identity tuple (search caught up for that exact
// provider/source-class/model/index-version combination), mirroring the
// supply_chain_impact_winners_materialization write in
// supply_chain_impact_canonical_winners_store.go. The row is keyed by the
// full identity tuple (not a singleton) so a ready publish for one identity
// can never satisfy freshness for a different identity — required during a
// provider, model, or vector-index-version rollout, or when two
// reducer/API configs share one Postgres.
const upsertSearchVectorBuildReadyQuery = `
INSERT INTO search_vector_build_materialization
  (provider_profile_id, source_class, embedding_model_id, vector_index_version, materialized_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider_profile_id, source_class, embedding_model_id, vector_index_version)
DO UPDATE SET materialized_at = EXCLUDED.materialized_at
`

// EshuSearchVectorBuildIdentity names the vector-identity tuple a
// search_vector_ready watermark row is scoped to. It mirrors
// reducer.SearchVectorBuildIdentity; the reducer package cannot import this
// storage package (it stays free of storage dependencies), so the tuple
// shape is duplicated deliberately, the same way
// EshuSearchVectorPendingRequest mirrors reducer.SearchVectorBuildPendingRequest.
type EshuSearchVectorBuildIdentity struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

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

// PublishSearchVectorReady upserts the identity-keyed watermark row with the
// current time. Called once per bounded sweep whose post-build re-check finds
// zero pending scopes for that identity; a rapid, repeated caught-up sweep
// simply re-stamps the same row, so the write is idempotent under retry or
// repeated invocation.
func (s EshuSearchVectorBuildReadyStore) PublishSearchVectorReady(
	ctx context.Context,
	identity EshuSearchVectorBuildIdentity,
) error {
	if s.db == nil {
		return fmt.Errorf("eshu search vector build ready store requires a database")
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertSearchVectorBuildReadyQuery,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("publish search vector ready watermark: %w", err)
	}
	return nil
}
