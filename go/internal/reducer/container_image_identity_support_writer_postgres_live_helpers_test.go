// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

func insertContainerImageIdentityLegacyLiveFact(
	db *sql.DB,
	scopeID string,
	generationID string,
	digest string,
) error {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, err := db.Exec(`
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    'legacy:v3-live', $1, $2, 'reducer_container_image_identity',
    'container_image_identity:legacy', 'git', 'legacy:intent', $4, $4,
    jsonb_build_object(
        'digest', $3::text,
        'image_ref', 'registry.example.com/team/legacy@' || $3::text,
        'repository_id', 'repository:legacy',
        'outcome', 'exact_digest',
        'source_repository_ids', jsonb_build_array('repository:legacy')
    )
)
`, scopeID, generationID, digest, now)
	return err
}

func seedContainerImageIdentityLiveScope(
	t *testing.T,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := db.Exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb);
`, scopeID, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	seedContainerImageIdentityLiveGeneration(t, db, scopeID, generationID, "active")
	if _, err := db.Exec(`
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generationID); err != nil {
		t.Fatalf("activate scope generation: %v", err)
	}
}

func seedContainerImageIdentityLiveGeneration(
	t *testing.T,
	db *sql.DB,
	scopeID string,
	generationID string,
	status string,
) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := db.Exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'test', $3, $3, $4, '{}'::jsonb)
`, generationID, scopeID, now, status); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
}

func seedContainerImageIdentityLiveWork(
	t *testing.T,
	db *sql.DB,
	intentID string,
	scopeID string,
	generationID string,
	claimEpoch int64,
) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := db.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    created_at, updated_at, container_image_identity_v2_required,
    container_image_identity_claim_epoch, container_image_identity_v2_authorized_status,
    container_image_identity_v3_required, container_image_identity_v3_authorized_status
) VALUES ($1, $2, $3, 'reducer', 'container_image_identity', 'claimed',
          $5, $5, TRUE, $4, 'claimed', TRUE, 'claimed')
`, intentID, scopeID, generationID, claimEpoch, now); err != nil {
		t.Fatalf("insert work item: %v", err)
	}
}

func completeContainerImageIdentityLiveWork(t *testing.T, db *sql.DB, intentID string) {
	t.Helper()
	if _, err := db.Exec(`
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = 'succeeded',
    container_image_identity_v3_authorized_status = 'succeeded'
WHERE work_item_id = $1
`, intentID); err != nil {
		t.Fatalf("complete work item %q: %v", intentID, err)
	}
}

func containerImageIdentityLiveEpoch(
	t *testing.T,
	db *sql.DB,
	scopeID string,
	generationID string,
) int64 {
	t.Helper()
	var epoch int64
	if err := db.QueryRow(`
SELECT activation_epoch
FROM container_image_identity_scope_state
WHERE scope_id = $1 AND active_generation_id = $2
`, scopeID, generationID).Scan(&epoch); err != nil {
		t.Fatalf("read activation epoch: %v", err)
	}
	return epoch
}

func containerImageIdentityLiveVisibleCount(t *testing.T, db *sql.DB, digest string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT count(*) FROM container_image_identity_current_supports WHERE digest = $1
`, digest).Scan(&count); err != nil {
		t.Fatalf("count current supports: %v", err)
	}
	return count
}

func containerImageIdentitySupportLiveWrite(
	intentID string,
	scopeID string,
	generationID string,
	claimEpoch int64,
	activationEpoch int64,
	digest string,
) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:        intentID,
		ClaimEpoch:      claimEpoch,
		ActivationEpoch: activationEpoch,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    "git",
		Cause:           "live_test",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0).UTC(),
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/app-empty@" + digest,
				Digest:           digest,
				RepositoryID:     "repository:example",
				Outcome:          ContainerImageIdentityExactDigest,
				CanonicalWrites:  1,
				IdentityStrength: "immutable_digest",
			},
			{
				ImageRef:                     "registry.example.com/team/app@" + digest,
				Digest:                       digest,
				RepositoryID:                 "repository:example",
				SourceRepositoryIDs:          []string{"repository:example"},
				BuildProvenanceRepositoryIDs: []string{"repository:example"},
				Outcome:                      ContainerImageIdentityExactDigest,
				CanonicalWrites:              1,
				IdentityStrength:             "immutable_digest",
			},
			{
				ImageRef:            "registry.example.com/team/app-shared@" + digest,
				Digest:              digest,
				RepositoryID:        "repository:second",
				SourceRepositoryIDs: []string{"repository:example", "repository:second"},
				Outcome:             ContainerImageIdentityExactDigest,
				CanonicalWrites:     1,
				IdentityStrength:    "immutable_digest",
			},
		},
	}
}

type containerImageIdentityLiveClaimedExecer struct {
	db *sql.DB
}

func (e containerImageIdentityLiveClaimedExecer) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, false, rows.Err()
	}
	var deleted int
	if err := rows.Scan(&deleted); err != nil {
		return 0, false, err
	}
	return deleted, true, rows.Err()
}

type containerImageIdentityLiveHeldSupportLoader struct {
	db *sql.DB
}

type containerImageIdentityActivatingHeldSupportLoader struct {
	base      containerImageIdentityLiveHeldSupportLoader
	afterLoad func() error
	loaded    int
}

func (loader *containerImageIdentityActivatingHeldSupportLoader) LoadHeldContainerImageIdentitySupports(
	ctx context.Context,
	scopeID string,
	generationID string,
	activationEpoch int64,
	imageRefs []string,
) ([]ContainerImageIdentityPriorSupport, error) {
	supports, err := loader.base.LoadHeldContainerImageIdentitySupports(
		ctx, scopeID, generationID, activationEpoch, imageRefs,
	)
	if err != nil {
		return nil, err
	}
	loader.loaded = len(supports)
	if loader.afterLoad != nil {
		if err := loader.afterLoad(); err != nil {
			return nil, err
		}
	}
	return supports, nil
}

func (loader containerImageIdentityLiveHeldSupportLoader) LoadHeldContainerImageIdentitySupports(
	ctx context.Context,
	scopeID string,
	generationID string,
	activationEpoch int64,
	imageRefs []string,
) ([]ContainerImageIdentityPriorSupport, error) {
	rows, err := loader.db.QueryContext(ctx, `
SELECT
    digest, image_ref, repository_id, outcome, identity_strength,
    source_revision, source_revision_provenance, reason, canonical_writes,
    source_repository_ids, build_provenance_repository_ids,
    base_image_for_repository_ids, workload_ids, service_ids, source_layers,
    evidence_fact_ids, missing_evidence
FROM container_image_identity_current_supports
WHERE scope_id = $1
  AND generation_id = $2
  AND activation_epoch = $3
  AND image_ref = ANY($4::TEXT[])
ORDER BY digest, image_ref, repository_id, outcome
`, scopeID, generationID, activationEpoch, imageRefs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var supports []ContainerImageIdentityPriorSupport
	for rows.Next() {
		var support ContainerImageIdentityPriorSupport
		if err := rows.Scan(
			&support.Digest,
			&support.ImageRef,
			&support.RepositoryID,
			&support.Outcome,
			&support.IdentityStrength,
			&support.SourceRevision,
			&support.SourceRevisionProvenance,
			&support.Reason,
			&support.CanonicalWrites,
			pgarray.Array(&support.SourceRepositoryIDs),
			pgarray.Array(&support.BuildProvenanceRepositoryIDs),
			pgarray.Array(&support.BaseImageForRepositoryIDs),
			pgarray.Array(&support.WorkloadIDs),
			pgarray.Array(&support.ServiceIDs),
			pgarray.Array(&support.SourceLayers),
			pgarray.Array(&support.EvidenceFactIDs),
			pgarray.Array(&support.MissingEvidence),
		); err != nil {
			return nil, err
		}
		supports = append(supports, support)
	}
	return supports, rows.Err()
}
