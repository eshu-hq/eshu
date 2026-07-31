// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
)

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
			pq.Array(&support.SourceRepositoryIDs),
			pq.Array(&support.BuildProvenanceRepositoryIDs),
			pq.Array(&support.BaseImageForRepositoryIDs),
			pq.Array(&support.WorkloadIDs),
			pq.Array(&support.ServiceIDs),
			pq.Array(&support.SourceLayers),
			pq.Array(&support.EvidenceFactIDs),
			pq.Array(&support.MissingEvidence),
		); err != nil {
			return nil, err
		}
		supports = append(supports, support)
	}
	return supports, rows.Err()
}
