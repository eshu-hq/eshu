// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fluxHelmReleaseRowEntity builds a FluxHelmRelease EntityRow with the
// chart/chartRef/namespace metadata fields the parser (flux_helm.go) actually
// emits. Exactly one of (sourceRefKind/sourceRefName) or
// (chartRefKind/chartRefName) is normally set; both empty means neither
// spec.chart nor spec.chartRef was set on the fixture manifest.
func fluxHelmReleaseRowEntity(uid, filePath, namespace, sourceRefKind, sourceRefName, sourceRefNamespace, chartRefKind, chartRefName, chartRefNamespace string) projector.EntityRow {
	meta := map[string]any{}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	if sourceRefKind != "" {
		meta["source_ref_kind"] = sourceRefKind
	}
	if sourceRefName != "" {
		meta["source_ref_name"] = sourceRefName
	}
	if sourceRefNamespace != "" {
		meta["source_ref_namespace"] = sourceRefNamespace
	}
	if chartRefKind != "" {
		meta["chart_ref_kind"] = chartRefKind
	}
	if chartRefName != "" {
		meta["chart_ref_name"] = chartRefName
	}
	if chartRefNamespace != "" {
		meta["chart_ref_namespace"] = chartRefNamespace
	}
	return projector.EntityRow{
		Label:      "FluxHelmRelease",
		EntityID:   uid,
		EntityName: "helmrelease",
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// TestFluxHelmReconcilesFromChartSourceRefT1NamespaceExact proves the common
// HelmRelease case: spec.chart.spec.sourceRef naming a HelmRepository
// resolves via the SAME T1 namespace-exact tier FluxKustomization uses, with
// reconciler_kind='HelmRelease' and via='chart_source_ref' stamped on the
// edge (both literal in the static template, not row-parameterized).
func TestFluxHelmReconcilesFromChartSourceRefT1NamespaceExact(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/helmrepository.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid}")
	if !strings.Contains(merge.Cypher, "FluxHelmRepository {uid: row.target_uid}") {
		t.Fatalf("expected the HelmRepository-target template, got: %s", merge.Cypher)
	}
	if !strings.Contains(merge.Cypher, "r.reconciler_kind = 'HelmRelease'") {
		t.Fatalf("expected literal reconciler_kind = 'HelmRelease', got: %s", merge.Cypher)
	}
	if !strings.Contains(merge.Cypher, "r.via = 'chart_source_ref'") {
		t.Fatalf("expected literal via = 'chart_source_ref', got: %s", merge.Cypher)
	}
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; %+v", len(rows), rows)
	}
	row := rows[0]
	if row["source_uid"] != "uid-hr" || row["target_uid"] != "uid-repo" {
		t.Fatalf("row = %+v, want uid-hr -> uid-repo", row)
	}
	if row["resolution_mode"] != "namespace_exact" {
		t.Fatalf("resolution_mode = %v, want namespace_exact", row["resolution_mode"])
	}
	if row["namespace_defaulted"] != false {
		t.Fatalf("namespace_defaulted = %v, want false", row["namespace_defaulted"])
	}
}

// TestFluxHelmReconcilesFromChartSourceRefNamespaceDefaultsFromHelmReleaseNamespace
// proves the namespace-default rule reuses the HelmRelease's OWN namespace
// (not the Kustomization's), mirroring resolveFluxReconciliationRows's
// reconciler-neutral namespace field.
func TestFluxHelmReconcilesFromChartSourceRefNamespaceDefaultsFromHelmReleaseNamespace(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/helmrepository.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxHelmRepository {uid: row.target_uid}")
	row := merge.Parameters["rows"].([]map[string]any)[0]
	if row["namespace_defaulted"] != true {
		t.Fatalf("namespace_defaulted = %v, want true", row["namespace_defaulted"])
	}
	if row["source_ref_namespace"] != "flux-system" {
		t.Fatalf("source_ref_namespace = %v, want flux-system (defaulted from the HelmRelease's own namespace)", row["source_ref_namespace"])
	}
}

// TestFluxHelmReconcilesFromChartSourceRefGitRepositoryAndBucketResolve proves
// chart.spec.sourceRef can also name a GitRepository or Bucket (mirroring
// FluxKustomization's sourceRef kind set for the chart-source-ref path).
func TestFluxHelmReconcilesFromChartSourceRefGitRepositoryAndBucketResolve(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr-git", "/repo/a.yaml", "flux-system", "GitRepository", "flux-system", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxGitRepository", "uid-git", "flux-system", "flux-system", "/repo/sources.yaml"),
			fluxHelmReleaseRowEntity("uid-hr-bucket", "/repo/b.yaml", "flux-system", "Bucket", "flux-artifacts", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxBucket", "uid-bucket", "flux-artifacts", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)

	gitMerge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid})\nMATCH (s:FluxGitRepository")
	gitRows := gitMerge.Parameters["rows"].([]map[string]any)
	if len(gitRows) != 1 || gitRows[0]["target_uid"] != "uid-git" {
		t.Fatalf("GitRepository rows = %+v, want one row targeting uid-git", gitRows)
	}

	bucketMerge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid})\nMATCH (s:FluxBucket")
	bucketRows := bucketMerge.Parameters["rows"].([]map[string]any)
	if len(bucketRows) != 1 || bucketRows[0]["target_uid"] != "uid-bucket" {
		t.Fatalf("Bucket rows = %+v, want one row targeting uid-bucket", bucketRows)
	}
}

// TestFluxHelmReconcilesFromChartRefOCIRepositoryResolves proves the
// chartRef path: spec.chartRef naming an OCIRepository resolves through the
// SAME T1-T4 tiers, with via='chart_ref' (never 'chart_source_ref') stamped
// on the edge.
func TestFluxHelmReconcilesFromChartRefOCIRepositoryResolves(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "", "", "", "OCIRepository", "podinfo-oci", "flux-system"),
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "podinfo-oci", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid})\nMATCH (s:FluxOCIRepository")
	if !strings.Contains(merge.Cypher, "r.via = 'chart_ref'") {
		t.Fatalf("expected literal via = 'chart_ref', got: %s", merge.Cypher)
	}
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["target_uid"] != "uid-oci" {
		t.Fatalf("rows = %+v, want one row targeting uid-oci", rows)
	}
}

// TestFluxHelmReconcilesFromChartRefHelmChartKindNeverLinks is the CRITICAL
// honest-non-link guard (issue #5483 C1 locked design): chartRef.kind
// "HelmChart" must NEVER produce an edge. Eshu's existing HelmChart label
// models a Chart.yaml DIRECTORY ((name,path) identity, schema_tables.go),
// NOT the Flux HelmChart custom resource -- fluxHelmChartRefKindToLabel
// deliberately omits "HelmChart" so this is an honest non-link, never a
// fabricated cross-class join between two unrelated graph identities that
// happen to share a name.
func TestFluxHelmReconcilesFromChartRefHelmChartKindNeverLinks(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "", "", "", "HelmChart", "podinfo", "flux-system"),
			// Even if a HelmChart-labeled node existed (it never does --
			// HelmChart is a Chart.yaml directory, not a Flux CR), no edge may
			// ever target it through this path.
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for chartRef.kind=HelmChart, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromBothChartAndChartRefSetSkips proves the
// exactly-one-of guard: a HelmRelease with BOTH a chart-derived sourceRef AND
// chart_ref_kind set (an invalid CR per the Flux API) produces NO edge --
// never an arbitrary pick between the two.
func TestFluxHelmReconcilesFromBothChartAndChartRefSetSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "OCIRepository", "podinfo-oci", "flux-system"),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "podinfo-oci", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement when both chart and chartRef are set, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromNeitherChartNorChartRefSetSkips proves the
// sibling guard: a HelmRelease with NEITHER a resolvable sourceRef NOR a
// chartRef (an incomplete/invalid CR) produces no row -- never a fabricated
// resolution from name alone.
func TestFluxHelmReconcilesFromNeitherChartNorChartRefSetSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "", "", "", "", "", ""),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement when neither chart nor chartRef is set, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromUnknownSourceRefKindSkips proves an unmapped
// chart.spec.sourceRef.kind is an honest non-link, mirroring
// TestFluxReconcilesFromUnknownSourceRefKindSkips for Kustomization.
func TestFluxHelmReconcilesFromUnknownSourceRefKindSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "ExternalArtifact", "podinfo", "flux-system", "", "", ""),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for an unknown sourceRef.kind, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromDanglingRefSkips proves a chartRef/sourceRef
// naming a CR absent from the repo produces no edge.
func TestFluxHelmReconcilesFromDanglingRefSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/helmrelease.yaml", "flux-system", "HelmRepository", "does-not-exist", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for a dangling ref, got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromAmbiguousTieSkips proves the T2 tie-skip case is
// reached identically for HelmRelease-sourced resolution (resolver reuse,
// not a reimplementation): two candidates in different files, equidistant
// from the HelmRelease's file, produce no edge.
func TestFluxHelmReconcilesFromAmbiguousTieSkips(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			fluxHelmReleaseRowEntity("uid-hr", "/repo/clusters/apps.yaml", "flux-system", "HelmRepository", "podinfo", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo-a", "podinfo", "flux-system", "/repo/clusters/prod/sources.yaml"),
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo-b", "podinfo", "flux-system", "/repo/clusters/staging/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for an unresolved tie, got %+v", stmt.Parameters["rows"])
		}
	}
}

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
