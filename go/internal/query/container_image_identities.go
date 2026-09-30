// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type containerImageIdentityQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresContainerImageIdentityStore reads active container image identity
// facts from Postgres using bounded payload predicates.
type PostgresContainerImageIdentityStore struct {
	DB     containerImageIdentityQueryer
	reader db.Queryer
}

// NewPostgresContainerImageIdentityStore creates the Postgres-backed
// container image identity read model.
func NewPostgresContainerImageIdentityStore(db containerImageIdentityQueryer) PostgresContainerImageIdentityStore {
	return PostgresContainerImageIdentityStore{DB: db}
}

// NewPostgresContainerImageIdentityStoreWithReadStore reads through a guarded
// query port that exposes no write method.
func NewPostgresContainerImageIdentityStoreWithReadStore(reader db.Queryer) PostgresContainerImageIdentityStore {
	return PostgresContainerImageIdentityStore{reader: reader}
}

// ListContainerImageIdentities returns one bounded page of active reducer
// container image identity facts.
func (s PostgresContainerImageIdentityStore) ListContainerImageIdentities(
	ctx context.Context,
	filter ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	if s.DB == nil && s.reader == nil {
		return nil, fmt.Errorf("container image identity database is required")
	}
	if !filter.HasScope() {
		return nil, fmt.Errorf("digest, image_ref, source_repository_id, repository_id, or outcome is required")
	}
	if filter.Limit <= 0 || filter.Limit > containerImageIdentityMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", containerImageIdentityMaxLimit+1)
	}

	queryer := s.reader
	if queryer == nil {
		queryer = sqlRowsQueryer{inner: s.DB}
	}
	rows, err := queryer.QueryContext(
		ctx,
		listContainerImageIdentitiesQuery,
		filter.Digest,
		filter.ImageRef,
		filter.SourceRepositoryID,
		filter.RepositoryID,
		filter.Outcome,
		filter.AfterIdentityID,
		filter.Limit,
		array.Of(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list container image identities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContainerImageIdentityRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list container image identities: %w", err)
		}
		row, err := decodeContainerImageIdentityRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list container image identities: %w", err)
	}
	return out, nil
}

func decodeContainerImageIdentityRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (ContainerImageIdentityRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ContainerImageIdentityRow{}, fmt.Errorf("decode container image identity: %w", err)
	}
	return ContainerImageIdentityRow{
		IdentityID:               factID,
		Digest:                   StringVal(payload, "digest"),
		ImageRef:                 StringVal(payload, "image_ref"),
		RepositoryID:             StringVal(payload, "repository_id"),
		SourceRepositoryIDs:      StringSliceVal(payload, "source_repository_ids"),
		SourceRevision:           StringVal(payload, "source_revision"),
		SourceRevisionProvenance: StringVal(payload, "source_revision_provenance"),
		WorkloadIDs:              StringSliceVal(payload, "workload_ids"),
		ServiceIDs:               StringSliceVal(payload, "service_ids"),
		Outcome:                  StringVal(payload, "outcome"),
		Reason:                   StringVal(payload, "reason"),
		IdentityStrength:         StringVal(payload, "identity_strength"),
		CanonicalID:              StringVal(payload, "canonical_id"),
		CanonicalWrites:          IntVal(payload, "canonical_writes"),
		SourceLayers:             StringSliceVal(payload, "source_layers"),
		EvidenceFactIDs:          StringSliceVal(payload, "evidence_fact_ids"),
		MissingEvidence:          StringSliceVal(payload, "missing_evidence"),
		SourceFreshness:          "active",
		SourceConfidence:         sourceConfidence,
	}, nil
}
