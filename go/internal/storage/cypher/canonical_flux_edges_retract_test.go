// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestFluxReconcilesFromOCIRepositoryAndBucketRefsResolve proves the
// resolution algorithm and per-label MERGE routing works identically for
// OCIRepository and Bucket sourceRef kinds, not just GitRepository.
func TestFluxReconcilesFromOCIRepositoryAndBucketRefsResolve(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k-oci", "/repo/apps.yaml", "flux-system", "OCIRepository", "app-manifests", "flux-system"),
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "app-manifests", "flux-system", "/repo/sources.yaml"),
			fluxKustomizationRowEntity("uid-k-bucket", "/repo/apps.yaml", "flux-system", "Bucket", "flux-artifacts", "flux-system"),
			fluxSourceRowEntity("FluxBucket", "uid-bucket", "flux-artifacts", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)

	ociMerge := mergeStatementContaining(t, stmts, "FluxOCIRepository {uid: row.target_uid}")
	ociRows := ociMerge.Parameters["rows"].([]map[string]any)
	if len(ociRows) != 1 || ociRows[0]["target_uid"] != "uid-oci" {
		t.Fatalf("OCIRepository rows = %+v, want one row targeting uid-oci", ociRows)
	}

	bucketMerge := mergeStatementContaining(t, stmts, "FluxBucket {uid: row.target_uid}")
	bucketRows := bucketMerge.Parameters["rows"].([]map[string]any)
	if len(bucketRows) != 1 || bucketRows[0]["target_uid"] != "uid-bucket" {
		t.Fatalf("Bucket rows = %+v, want one row targeting uid-bucket", bucketRows)
	}
}

// TestFluxReconcilesFromUnknownSourceRefKindSkips proves a sourceRef.kind
// outside the closed {GitRepository, OCIRepository, Bucket} map (e.g. the
// feature-gated ExternalArtifact) is an honest non-link, never guessed at.
func TestFluxReconcilesFromUnknownSourceRefKindSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "ExternalArtifact", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for an unknown sourceRef.kind, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromRetractsStaleEdgesBeforeMerge proves the builder
// emits a Drain-marked, generation-scoped retraction for RECONCILES_FROM
// BEFORE the MERGE statements, covering the case where the Kustomization
// survives but its resolved source changes.
func TestFluxReconcilesFromRetractsStaleEdgesBeforeMerge(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	if len(stmts) != 2 {
		t.Fatalf("fluxReconcilesFromEdgeStatements() returned %d statements, want 2 (1 retract + 1 merge)", len(stmts))
	}

	retract := stmts[0]
	if retract.Operation != OperationCanonicalRetract {
		t.Fatalf("statement 0 Operation = %q, want %q", retract.Operation, OperationCanonicalRetract)
	}
	if !retract.Drain {
		t.Fatal("retract statement has Drain = false, want true (grouped UNWIND relationship DELETE no-ops on NornicDB, #4476)")
	}
	if !strings.Contains(retract.Cypher, "FluxKustomization {uid: uid}") ||
		!strings.Contains(retract.Cypher, "[r:RECONCILES_FROM]") ||
		!strings.Contains(retract.Cypher, "r.evidence_source = 'projector/canonical'") ||
		!strings.Contains(retract.Cypher, "r.generation_id <> $generation_id") ||
		!strings.Contains(retract.Cypher, "DELETE r") {
		t.Fatalf("retract cypher wrong shape: %s", retract.Cypher)
	}
	sourceUIDs, ok := retract.Parameters["source_uids"].([]string)
	if !ok || len(sourceUIDs) != 1 || sourceUIDs[0] != "uid-k" {
		t.Fatalf("retract source_uids = %#v, want [uid-k]", retract.Parameters["source_uids"])
	}
	if retract.Parameters["generation_id"] != "gen-2" {
		t.Fatalf("retract generation_id = %v, want gen-2", retract.Parameters["generation_id"])
	}

	if stmts[1].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[1].Cypher, "FluxGitRepository {uid: row.target_uid}") {
		t.Fatalf("statement 1 should be the FluxGitRepository merge: op=%q cypher=%s", stmts[1].Operation, stmts[1].Cypher)
	}
}

// TestFluxReconcilesFromRetractCoversAllKustomizationUIDsRegardlessOfResolvability
// proves the retract scope includes EVERY FluxKustomization uid in the
// materialization, not just the ones that resolve this generation -- a
// Kustomization whose sourceRef became dangling/unknown-kind this generation
// must still have its prior-generation RECONCILES_FROM edge retracted.
func TestFluxReconcilesFromRetractCoversAllKustomizationUIDsRegardlessOfResolvability(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k-resolves", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
			// This one no longer has a resolvable sourceRef (kind now unknown),
			// but its uid must still be retract-scoped.
			fluxKustomizationRowEntity("uid-k-no-longer-resolves", "/repo/other.yaml", "flux-system", "ExternalArtifact", "flux-system", "flux-system"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	retract := stmts[0]
	sourceUIDs, _ := retract.Parameters["source_uids"].([]string)
	want := map[string]bool{"uid-k-resolves": true, "uid-k-no-longer-resolves": true}
	if len(sourceUIDs) != len(want) {
		t.Fatalf("retract source_uids = %#v, want both uids", sourceUIDs)
	}
	for _, uid := range sourceUIDs {
		if !want[uid] {
			t.Fatalf("unexpected retract source uid %q", uid)
		}
	}
}

// TestFluxReconcilesFromFirstGenerationSkipsStaleEdgeRetract proves the
// generation-scoped cleanup preserves first-generation behavior, mirroring
// TestAtlantisEdgeStatementsFirstGenerationSkipsStaleEdgeRetract.
func TestFluxReconcilesFromFirstGenerationSkipsStaleEdgeRetract(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		FirstGeneration: true,
		GenerationID:    "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	if len(stmts) != 1 {
		t.Fatalf("fluxReconcilesFromEdgeStatements() returned %d statements, want 1 merge-only statement", len(stmts))
	}
	if stmts[0].Operation != OperationCanonicalUpsert {
		t.Fatalf("statement 0 should not retract on first generation: %+v", stmts[0])
	}
}

// TestFluxReconcilesFromNilWithoutFluxKustomization proves the builder is a
// no-op for materializations that carry no FluxKustomization entity.
func TestFluxReconcilesFromNilWithoutFluxKustomization(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			{Label: "Function", EntityID: "fn-1"},
		},
	}
	if stmts := fluxReconcilesFromEdgeStatements(mat); stmts != nil {
		t.Fatalf("fluxReconcilesFromEdgeStatements() = %d statements, want nil for non-Flux repo", len(stmts))
	}
}

// TestFluxRelationshipMaterializedEdgeTypesReturnsDefensiveCopy proves the
// exported accessor returns a defensive copy and covers RECONCILES_FROM,
// mirroring CrossplaneRelationshipMaterializedEdgeTypes's contract.
func TestFluxRelationshipMaterializedEdgeTypesReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	got := FluxRelationshipMaterializedEdgeTypes()
	if _, ok := got["RECONCILES_FROM"]; !ok {
		t.Fatal("FluxRelationshipMaterializedEdgeTypes() missing RECONCILES_FROM")
	}
	got["RECONCILES_FROM"] = "mutated"
	again := FluxRelationshipMaterializedEdgeTypes()
	if again["RECONCILES_FROM"] == "mutated" {
		t.Fatal("FluxRelationshipMaterializedEdgeTypes() returned mutable backing storage")
	}
}
