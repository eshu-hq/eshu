// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerabilitysuppression/v1"
)

// PostgresVulnerabilitySuppressionMutationStore adapts the atomic storage
// writer to the query mutation contract.
type PostgresVulnerabilitySuppressionMutationStore struct {
	store *postgres.VulnerabilitySuppressionStore
	now   func() time.Time
}

// NewPostgresVulnerabilitySuppressionMutationStore creates a Postgres-backed
// vulnerability suppression mutation store.
func NewPostgresVulnerabilitySuppressionMutationStore(
	db *sql.DB,
) *PostgresVulnerabilitySuppressionMutationStore {
	return &PostgresVulnerabilitySuppressionMutationStore{
		store: postgres.NewVulnerabilitySuppressionStore(postgres.SQLDB{DB: db}),
		now:   time.Now,
	}
}

// UpsertVulnerabilitySuppression persists a complete typed operator payload.
func (s *PostgresVulnerabilitySuppressionMutationStore) UpsertVulnerabilitySuppression(
	ctx context.Context,
	value vulnerabilitysuppressionv1.Suppression,
) (VulnerabilitySuppressionMutationResult, error) {
	if s == nil || s.store == nil {
		return VulnerabilitySuppressionMutationResult{}, fmt.Errorf("vulnerability suppression mutation store is required")
	}
	payload, err := factschema.EncodeVulnerabilitySuppression(value)
	if err != nil {
		return VulnerabilitySuppressionMutationResult{}, fmt.Errorf("encode vulnerability suppression: %w", err)
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	result, err := s.store.Upsert(ctx, postgres.VulnerabilitySuppressionMutation{
		SuppressionID: value.SuppressionID,
		Payload:       payload,
		ObservedAt:    now().UTC(),
	})
	if err != nil {
		return VulnerabilitySuppressionMutationResult{}, err
	}
	return VulnerabilitySuppressionMutationResult{
		SuppressionID: result.SuppressionID,
		GenerationID:  result.GenerationID,
		Changed:       result.Changed,
	}, nil
}
