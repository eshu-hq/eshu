// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

const containerImageIdentityFactKind = "reducer_container_image_identity"

// ContainerImageIdentityStore reads reducer-owned container image identity
// facts.
type ContainerImageIdentityStore interface {
	ListContainerImageIdentities(context.Context, ContainerImageIdentityFilter) ([]ContainerImageIdentityRow, error)
}

// ContainerImageIdentityFilter bounds identity reads to an image digest, image
// reference, source repository, OCI repository, or reducer outcome.
type ContainerImageIdentityFilter struct {
	Digest             string
	ImageRef           string
	SourceRepositoryID string
	RepositoryID       string
	Outcome            string
	AfterIdentityID    string
	Limit              int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (the union
	// of granted repository and ingestion-scope ids). When empty the read is
	// unrestricted (shared token, all-scope admin, or local dev mode). When
	// populated the query keeps only identities whose source_repository_ids
	// overlap the granted set, so a scoped caller never sees image identities
	// it cannot attribute to a granted git repository. Identity facts key on
	// the OCI repository_id and an OCI registry ingestion scope, neither of
	// which is a durable join to a git-repo grant, so source_repository_ids
	// overlap is the only correct attribution and uncorrelated images stay
	// invisible to scoped tokens.
	AllowedSourceRepositoryIDs []string
}

// ContainerImageIdentityRow is one durable image identity fact decoded from
// the reducer-owned read model.
type ContainerImageIdentityRow struct {
	IdentityID          string
	Digest              string
	ImageRef            string
	RepositoryID        string
	SourceRepositoryIDs []string
	SourceRevision      string
	// SourceRevisionProvenance names where SourceRevision came from
	// ("oci_config_source_label" or "ci_run_commit"), letting a consumer keep
	// the in-image-label tier distinct from the weaker CI-run-commit fallback
	// (#5423). Empty when no revision was resolved.
	SourceRevisionProvenance string
	WorkloadIDs              []string
	ServiceIDs               []string
	Outcome                  string
	Reason                   string
	IdentityStrength         string
	CanonicalID              string
	CanonicalWrites          int
	SourceLayers             []string
	EvidenceFactIDs          []string
	MissingEvidence          []string
	SourceFreshness          string
	SourceConfidence         string
}

type containerImageIdentityQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresContainerImageIdentityStore reads active container image identity
// facts from Postgres using bounded payload predicates.
type PostgresContainerImageIdentityStore struct {
	DB containerImageIdentityQueryer
}

// NewPostgresContainerImageIdentityStore creates the Postgres-backed
// container image identity read model.
func NewPostgresContainerImageIdentityStore(db containerImageIdentityQueryer) PostgresContainerImageIdentityStore {
	return PostgresContainerImageIdentityStore{DB: db}
}

// ListContainerImageIdentities returns one bounded page of active reducer
// container image identity facts.
func (s PostgresContainerImageIdentityStore) ListContainerImageIdentities(
	ctx context.Context,
	filter ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("container image identity database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("digest, image_ref, source_repository_id, repository_id, or outcome is required")
	}
	if filter.Limit <= 0 || filter.Limit > containerImageIdentityMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", containerImageIdentityMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listContainerImageIdentitiesQuery,
		containerImageIdentityFactKind,
		filter.Digest,
		filter.ImageRef,
		filter.SourceRepositoryID,
		filter.RepositoryID,
		filter.Outcome,
		filter.AfterIdentityID,
		filter.Limit,
		pq.Array(filter.AllowedSourceRepositoryIDs),
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

const listContainerImageIdentitiesQuery = `
SELECT fact.fact_id, fact.source_confidence, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'digest' = $2)
  AND ($3 = '' OR fact.payload->>'image_ref' = $3)
  AND ($4 = '' OR fact.payload->'source_repository_ids' ? $4)
  AND ($5 = '' OR fact.payload->>'repository_id' = $5)
  AND ($6 = '' OR fact.payload->>'outcome' = $6)
  AND ($7 = '' OR fact.fact_id > $7)
  AND (
        COALESCE(cardinality($9::text[]), 0) = 0
        OR fact.payload->'source_repository_ids' ?| $9::text[]
      )
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f ContainerImageIdentityFilter) hasScope() bool {
	return f.Digest != "" || f.ImageRef != "" || f.SourceRepositoryID != "" ||
		f.RepositoryID != "" || f.Outcome != ""
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
