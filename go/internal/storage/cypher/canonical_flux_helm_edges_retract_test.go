// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestFluxHelmReconcilesFromRetractsHelmReleaseAnchoredEdgesBeforeMerge
// proves the HelmRelease-anchored retract statement is emitted (Drain=true,
// anchored on FluxHelmRelease -- distinct from the FluxKustomization-anchored
// retract), BEFORE the MERGE statements.
func TestFluxHelmReconcilesFromRetractsHelmReleaseAnchoredEdgesBeforeMerge(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
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
		t.Fatal("HelmRelease-anchored retract statement has Drain = false, want true (Drain from first commit, mirroring the Kustomization-anchored retract, #4476)")
	}
	if !strings.Contains(retract.Cypher, "FluxHelmRelease {uid: uid}") ||
		!strings.Contains(retract.Cypher, "[r:RECONCILES_FROM]") ||
		!strings.Contains(retract.Cypher, "r.evidence_source = 'projector/canonical'") ||
		!strings.Contains(retract.Cypher, "r.generation_id <> $generation_id") ||
		!strings.Contains(retract.Cypher, "DELETE r") {
		t.Fatalf("retract cypher wrong shape: %s", retract.Cypher)
	}
	sourceUIDs, ok := retract.Parameters["source_uids"].([]string)
	if !ok || len(sourceUIDs) != 1 || sourceUIDs[0] != "uid-hr" {
		t.Fatalf("retract source_uids = %#v, want [uid-hr]", retract.Parameters["source_uids"])
	}

	if stmts[1].Operation != OperationCanonicalUpsert {
		t.Fatalf("statement 1 should be the HelmRepository merge: op=%q", stmts[1].Operation)
	}
}

// TestFluxHelmReconcilesFromRetractCoversAllHelmReleaseUIDsRegardlessOfResolvability
// mirrors the Kustomization sibling: the HelmRelease retract scope includes
// EVERY FluxHelmRelease uid, not just the ones that resolve this generation.
func TestFluxHelmReconcilesFromRetractCoversAllHelmReleaseUIDsRegardlessOfResolvability(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr-resolves", "/repo/a.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
			fluxHelmReleaseRowEntity("uid-hr-no-longer-resolves", "/repo/b.yaml", "flux-system", "", "", "", "HelmChart", "podinfo", "flux-system"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	var retract Statement
	for _, s := range stmts {
		if s.Operation == OperationCanonicalRetract && strings.Contains(s.Cypher, "FluxHelmRelease {uid: uid}") {
			retract = s
		}
	}
	sourceUIDs, _ := retract.Parameters["source_uids"].([]string)
	want := map[string]bool{"uid-hr-resolves": true, "uid-hr-no-longer-resolves": true}
	if len(sourceUIDs) != len(want) {
		t.Fatalf("retract source_uids = %#v, want both uids", sourceUIDs)
	}
	for _, uid := range sourceUIDs {
		if !want[uid] {
			t.Fatalf("unexpected retract source uid %q", uid)
		}
	}
}

// TestFluxHelmReconcilesFromFirstGenerationSkipsStaleEdgeRetract mirrors the
// Kustomization sibling for first-generation behavior.
func TestFluxHelmReconcilesFromFirstGenerationSkipsStaleEdgeRetract(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		FirstGeneration: true,
		GenerationID:    "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
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

// TestFluxHelmReconcilesFromNilWithoutFluxHelmRelease proves the builder is a
// no-op contribution when the materialization carries no FluxHelmRelease
// entity (Kustomization-only repos see no behavior change from this feature).
func TestFluxHelmReconcilesFromNilWithoutFluxHelmRelease(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			{Label: "Function", EntityID: "fn-1"},
		},
	}
	if stmts := fluxReconcilesFromEdgeStatements(mat); stmts != nil {
		t.Fatalf("fluxReconcilesFromEdgeStatements() = %d statements, want nil for a repo with no Flux reconciler entities", len(stmts))
	}
}

// TestFluxHelmReconcilesFromKustomizationAndHelmReleaseCoexistIndependently
// proves both reconciler kinds resolve correctly in the SAME materialization
// without cross-contaminating each other's retract scope or Cypher template
// selection.
func TestFluxHelmReconcilesFromKustomizationAndHelmReleaseCoexistIndependently(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)

	kMerge := mergeStatementContaining(t, stmts, "FluxKustomization {uid: row.source_uid}")
	kRows := kMerge.Parameters["rows"].([]map[string]any)
	if len(kRows) != 1 || kRows[0]["target_uid"] != "uid-gr" {
		t.Fatalf("Kustomization rows = %+v, want one row targeting uid-gr", kRows)
	}
	if !strings.Contains(kMerge.Cypher, "r.reconciler_kind = 'Kustomization'") {
		t.Fatalf("Kustomization merge cypher missing literal reconciler_kind = 'Kustomization': %s", kMerge.Cypher)
	}

	hrMerge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid}")
	hrRows := hrMerge.Parameters["rows"].([]map[string]any)
	if len(hrRows) != 1 || hrRows[0]["target_uid"] != "uid-repo" {
		t.Fatalf("HelmRelease rows = %+v, want one row targeting uid-repo", hrRows)
	}
}

// TestFluxHelmRelationshipMaterializedEdgeTypesStillCoversReconcilesFrom
// proves the exported write-reasons accessor still advertises RECONCILES_FROM
// (unchanged map key) after the reason text was updated to cover both
// reconciler kinds.
func TestFluxHelmRelationshipMaterializedEdgeTypesStillCoversReconcilesFrom(t *testing.T) {
	t.Parallel()

	got := FluxRelationshipMaterializedEdgeTypes()
	reason, ok := got["RECONCILES_FROM"]
	if !ok {
		t.Fatal("FluxRelationshipMaterializedEdgeTypes() missing RECONCILES_FROM")
	}
	if !strings.Contains(reason, "FluxHelmRelease") {
		t.Fatalf("RECONCILES_FROM reason = %q, want it to mention FluxHelmRelease", reason)
	}
}
