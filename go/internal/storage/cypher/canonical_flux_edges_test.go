// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fluxKustomizationRowEntity builds a FluxKustomization EntityRow with the
// sourceRef/namespace metadata fields the parser (flux.go) actually emits.
func fluxKustomizationRowEntity(uid, filePath, namespace, refKind, refName, refNamespace string) projector.EntityRow {
	meta := map[string]any{}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	if refKind != "" {
		meta["source_ref_kind"] = refKind
	}
	if refName != "" {
		meta["source_ref_name"] = refName
	}
	if refNamespace != "" {
		meta["source_ref_namespace"] = refNamespace
	}
	return projector.EntityRow{
		Label:      "FluxKustomization",
		EntityID:   uid,
		EntityName: "kustomization",
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// fluxSourceRowEntity builds a FluxGitRepository/FluxOCIRepository/FluxBucket
// EntityRow with the fields sourceRef resolution needs. name == "" mirrors a
// generateName-only CR (never joinable).
func fluxSourceRowEntity(label, uid, name, namespace, filePath string) projector.EntityRow {
	meta := map[string]any{}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	return projector.EntityRow{
		Label:      label,
		EntityID:   uid,
		EntityName: name,
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// TestFluxReconcilesFromT1NamespaceExact proves the single-candidate,
// declared-namespace-match case (the common Flux layout): a Kustomization
// whose sourceRef.namespace matches exactly one GitRepository's declared
// namespace resolves with mode "namespace_exact" and namespace_defaulted=false.
func TestFluxReconcilesFromT1NamespaceExact(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; %+v", len(rows), rows)
	}
	row := rows[0]
	if row["source_uid"] != "uid-k" || row["target_uid"] != "uid-gr" {
		t.Fatalf("row = %+v, want uid-k -> uid-gr", row)
	}
	if row["resolution_mode"] != "namespace_exact" {
		t.Fatalf("resolution_mode = %v, want namespace_exact", row["resolution_mode"])
	}
	if row["namespace_defaulted"] != false {
		t.Fatalf("namespace_defaulted = %v, want false", row["namespace_defaulted"])
	}
	if row["source_ref_namespace"] != "flux-system" {
		t.Fatalf("source_ref_namespace = %v, want flux-system", row["source_ref_namespace"])
	}
}

// TestFluxReconcilesFromNamespaceDefaultedFromKustomizationNamespace proves
// Flux's own default rule: an empty sourceRef.namespace falls back to the
// Kustomization's own metadata.namespace, and namespace_defaulted is stamped
// true on the resulting edge.
func TestFluxReconcilesFromNamespaceDefaultedFromKustomizationNamespace(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", ""),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	row := merge.Parameters["rows"].([]map[string]any)[0]
	if row["namespace_defaulted"] != true {
		t.Fatalf("namespace_defaulted = %v, want true", row["namespace_defaulted"])
	}
	if row["source_ref_namespace"] != "flux-system" {
		t.Fatalf("source_ref_namespace = %v, want flux-system (defaulted from Kustomization namespace)", row["source_ref_namespace"])
	}
	if row["resolution_mode"] != "namespace_exact" {
		t.Fatalf("resolution_mode = %v, want namespace_exact", row["resolution_mode"])
	}
}

// TestFluxReconcilesFromT2SameFileWins proves the multi-cluster duplicate
// disambiguation prefers a candidate declared in the SAME FILE as the
// Kustomization (the gotk-sync.yaml layout) over a candidate in another file,
// even when both share (kind, name, namespace).
func TestFluxReconcilesFromT2SameFileWins(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/clusters/prod/gotk-sync.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-same-file", "flux-system", "flux-system", "/repo/clusters/prod/gotk-sync.yaml"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-other-file", "flux-system", "flux-system", "/repo/clusters/staging/gotk-sync.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; %+v", len(rows), rows)
	}
	if rows[0]["target_uid"] != "uid-gr-same-file" {
		t.Fatalf("target_uid = %v, want uid-gr-same-file (same-file preference)", rows[0]["target_uid"])
	}
	if rows[0]["resolution_mode"] != "namespace_exact_nearest_path" {
		t.Fatalf("resolution_mode = %v, want namespace_exact_nearest_path", rows[0]["resolution_mode"])
	}
}

// TestFluxReconcilesFromT2NearestPathWins proves that when no candidate
// shares the Kustomization's exact file, the candidate with the longest
// common directory prefix wins.
func TestFluxReconcilesFromT2NearestPathWins(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/clusters/prod/apps/kustomization.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-near", "flux-system", "flux-system", "/repo/clusters/prod/sources.yaml"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-far", "flux-system", "flux-system", "/repo/clusters/staging/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; %+v", len(rows), rows)
	}
	if rows[0]["target_uid"] != "uid-gr-near" {
		t.Fatalf("target_uid = %v, want uid-gr-near (nearest common directory prefix)", rows[0]["target_uid"])
	}
	if rows[0]["resolution_mode"] != "namespace_exact_nearest_path" {
		t.Fatalf("resolution_mode = %v, want namespace_exact_nearest_path", rows[0]["resolution_mode"])
	}
}

// TestFluxReconcilesFromT2TieSkips proves an unresolved multi-cluster tie (two
// candidates in different files, equidistant from the Kustomization's file)
// produces NO edge -- never a representative pick.
func TestFluxReconcilesFromT2TieSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/clusters/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-a", "flux-system", "flux-system", "/repo/clusters/prod/sources.yaml"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-b", "flux-system", "flux-system", "/repo/clusters/staging/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, "FluxGitRepository {uid: row.target_uid}") {
			t.Fatalf("expected no MERGE statement for an unresolved tie, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromT3AbsentNamespaceCandidateUnique proves T3: refNS is
// known but the only candidate has an absent (apply-time-injected) namespace
// -- resolves with mode "name_unique_namespace_unknown".
func TestFluxReconcilesFromT3AbsentNamespaceCandidateUnique(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	row := merge.Parameters["rows"].([]map[string]any)[0]
	if row["resolution_mode"] != "name_unique_namespace_unknown" {
		t.Fatalf("resolution_mode = %v, want name_unique_namespace_unknown", row["resolution_mode"])
	}
	if row["target_uid"] != "uid-gr" {
		t.Fatalf("target_uid = %v, want uid-gr", row["target_uid"])
	}
}

// TestFluxReconcilesFromT3AmbiguousAbsentNamespaceSkips proves 2+
// absent-namespace candidates for the same (kind,name) with zero
// declared-namespace matches skip rather than pick one.
func TestFluxReconcilesFromT3AmbiguousAbsentNamespaceSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-1", "flux-system", "", "/repo/sources-a.yaml"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-2", "flux-system", "", "/repo/sources-b.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, "FluxGitRepository {uid: row.target_uid}") {
			t.Fatalf("expected no MERGE for ambiguous absent-namespace candidates, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromT4RefNamespaceFullyUnknownUniqueNameResolves proves
// T4: both sourceRef.namespace and the Kustomization's own namespace are
// absent (refNS fully unknown), but exactly one (kind,name) candidate exists
// repo-wide -- resolves with mode "name_unique_namespace_unknown".
func TestFluxReconcilesFromT4RefNamespaceFullyUnknownUniqueNameResolves(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", ""),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "some-namespace", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxGitRepository {uid: row.target_uid}")
	row := merge.Parameters["rows"].([]map[string]any)[0]
	if row["resolution_mode"] != "name_unique_namespace_unknown" {
		t.Fatalf("resolution_mode = %v, want name_unique_namespace_unknown", row["resolution_mode"])
	}
	if row["namespace_defaulted"] != true {
		t.Fatalf("namespace_defaulted = %v, want true", row["namespace_defaulted"])
	}
	if row["source_ref_namespace"] != nil {
		t.Fatalf("source_ref_namespace = %v, want nil (omitted, fully unknown)", row["source_ref_namespace"])
	}
}

// TestFluxReconcilesFromT4AmbiguousRepoWideSkips proves T4's ambiguous case:
// refNS fully unknown and 2+ (kind,name) candidates exist repo-wide -- skip.
func TestFluxReconcilesFromT4AmbiguousRepoWideSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", ""),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-1", "flux-system", "namespace-a", "/repo/sources-a.yaml"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr-2", "flux-system", "namespace-b", "/repo/sources-b.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, "FluxGitRepository {uid: row.target_uid}") {
			t.Fatalf("expected no MERGE for a fully-unknown-namespace repo-wide ambiguity, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromDanglingRefSkips proves a sourceRef naming a CR that
// does not exist anywhere in the repo produces NO edge (never a fabricated
// join).
func TestFluxReconcilesFromDanglingRefSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "does-not-exist", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for a dangling sourceRef, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromDeclaredNamespaceMismatchSkips proves a candidate that
// exists under a DIFFERENT declared namespace than the (defaulted or
// explicit) sourceRef namespace produces NO edge -- a declared mismatch is not
// an absent-namespace case and must never fall through to a false join.
func TestFluxReconcilesFromDeclaredNamespaceMismatchSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "other-namespace", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for a declared-namespace mismatch, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxReconcilesFromKustomizationEmptySourceRefNameNeverResolves proves a
// Kustomization with an empty sourceRef.name is excluded from resolution
// entirely (collectFluxKustomizationEntities), never merely producing zero
// candidates downstream.
func TestFluxReconcilesFromKustomizationEmptySourceRefNameNeverResolves(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for an empty sourceRef.name, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestCollectFluxSourceEntitiesExcludesEmptyNameCandidates proves the
// candidate-collection guard directly: a generateName-only source CR (empty
// EntityName) is never inserted into the (label,name)-keyed candidate map,
// even though its FilePath/namespace fields are otherwise well-formed. This is
// the flux.md-documented invariant that an empty-name source must never
// false-join a Kustomization whose sourceRef.name is also empty -- exercised
// directly since the only current caller (collectFluxKustomizationEntities)
// separately guarantees no Kustomization ever reaches resolution with an
// empty sourceRef.name, making the two guards intentionally redundant
// (defense-in-depth), not mutually exclusive.
func TestCollectFluxSourceEntitiesExcludesEmptyNameCandidates(t *testing.T) {
	t.Parallel()

	entities := []projector.EntityRow{
		fluxSourceRowEntity("FluxGitRepository", "uid-named", "flux-system", "flux-system", "/repo/sources.yaml"),
		fluxSourceRowEntity("FluxGitRepository", "uid-generated", "", "flux-system", "/repo/sources.yaml"),
	}

	got := collectFluxSourceEntities(entities)

	if candidates := got["FluxGitRepository\x00flux-system"]; len(candidates) != 1 || candidates[0].uid != "uid-named" {
		t.Fatalf("candidates for flux-system = %+v, want exactly [uid-named]", candidates)
	}
	if candidates, ok := got["FluxGitRepository\x00"]; ok || len(candidates) != 0 {
		t.Fatalf("empty-name key must never be populated, got %+v", candidates)
	}
}
