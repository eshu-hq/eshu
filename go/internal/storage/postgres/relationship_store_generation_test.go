// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestRelationshipStoreEmptyActiveGenerationHidesPriorResolvedRelationships(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	store := NewRelationshipStore(db)
	ctx := context.Background()

	if err := store.ActivateResolutionGeneration(ctx, "gen-old", "scope-deploy"); err != nil {
		t.Fatalf("ActivateResolutionGeneration(old): %v", err)
	}
	if err := store.UpsertResolved(ctx, "gen-old", []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repository:r_deploy",
			TargetRepoID:     "repository:r_service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.95,
			EvidenceCount:    1,
			Rationale:        "deployment file referenced service repo",
			ResolutionSource: relationships.ResolutionSourceInferred,
		},
	}); err != nil {
		t.Fatalf("UpsertResolved(old): %v", err)
	}

	if err := store.ActivateResolutionGeneration(ctx, "gen-empty", "scope-deploy"); err != nil {
		t.Fatalf("ActivateResolutionGeneration(empty): %v", err)
	}

	result, err := store.GetResolvedRelationships(ctx, "scope-deploy")
	if err != nil {
		t.Fatalf("GetResolvedRelationships: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("len active resolved = %d, want 0 after empty active generation: %#v", len(result), result)
	}
}
