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

// TestFluxHelmReconcilesFromChartBlockWithoutSourceRefPlusChartRefSkips is the
// P3-1 accuracy-hardening guard: a doubly-malformed HelmRelease that carries a
// spec.chart block (spec.chart.spec.chart set, so the `chart` field is
// captured) WITHOUT a sourceRef AND ALSO sets spec.chartRef. The chart block
// makes chart/chartRef mutually exclusive per the Flux API, so Flux rejects
// the CR at admission and it never reconciles -- writing a chartRef edge for
// it would be an accuracy defect (an edge for a CR that can never reconcile).
// The both-set guard keys on the chart block's presence (chart != ""), not
// only on a resolvable sourceRef name, so this is an honest non-link.
func TestFluxHelmReconcilesFromChartBlockWithoutSourceRefPlusChartRefSkips(t *testing.T) {
	t.Parallel()

	// Build the row inline: `chart` metadata present (a chart block), NO
	// sourceRef fields, chartRef set. fluxHelmReleaseRowEntity does not set the
	// `chart` field, so this case must be constructed directly.
	helmRelease := projector.EntityRow{
		Label:      "FluxHelmRelease",
		EntityID:   "uid-hr",
		EntityName: "helmrelease",
		FilePath:   "/repo/helmrelease.yaml",
		Metadata: map[string]any{
			"namespace":           "flux-system",
			"chart":               "podinfo",
			"chart_ref_kind":      "OCIRepository",
			"chart_ref_name":      "podinfo-oci",
			"chart_ref_namespace": "flux-system",
		},
	}

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			helmRelease,
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "podinfo-oci", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for a chart-block-plus-chartRef malformed CR (Flux rejects it; it never reconciles), got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromEmptyChartBlockPlusChartRefSkips is the codex-P1 /
// reviewer-P2 accuracy fix (extends P3-1): a triply-malformed HelmRelease
// whose spec.chart block is PRESENT but empty (no chart name, no sourceRef --
// so the parser captures neither `chart` nor `source_ref_name`, only the
// `chart_present` presence signal) WHILE spec.chartRef is also set. Flux
// rejects this CR at admission (chart/chartRef mutually exclusive), so it
// never reconciles; writing a chartRef edge for it is an accuracy defect.
// P3-1's guard (chart != "" || source_ref_name != "") could not see the empty
// chart block and would write the chartRef edge -- this asserts the
// chart_present-keyed guard suppresses it. RED before the chart_present fix
// (an edge is written), GREEN after (honest non-link).
func TestFluxHelmReconcilesFromEmptyChartBlockPlusChartRefSkips(t *testing.T) {
	t.Parallel()

	// spec.chart present-but-empty: only chart_present is set, NOT chart or any
	// source_ref_* field. spec.chartRef is also set. fluxHelmReleaseRowEntity
	// does not set chart_present, so build the row directly.
	helmRelease := projector.EntityRow{
		Label:      "FluxHelmRelease",
		EntityID:   "uid-hr",
		EntityName: "helmrelease",
		FilePath:   "/repo/helmrelease.yaml",
		Metadata: map[string]any{
			"namespace":           "flux-system",
			"chart_present":       "true",
			"chart_ref_kind":      "OCIRepository",
			"chart_ref_name":      "podinfo-oci",
			"chart_ref_namespace": "flux-system",
		},
	}

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			helmRelease,
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "podinfo-oci", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			t.Fatalf("expected no MERGE statement for an empty-spec.chart-block-plus-chartRef malformed CR (Flux rejects it; it never reconciles), got %+v", stmt.Parameters["rows"])
		}
	}
}

// TestFluxHelmReconcilesFromWellFormedChartWithChartPresentStillResolves is
// the golden-neutrality proof: the real kubernetes_comprehensive
// flux-helmrelease.yaml fixture has a populated spec.chart block, so the
// parser now stamps chart_present="true" on it. This proves that a well-formed
// CR (chart_present + sourceRef, NO chartRef) still resolves its edge
// identically -- the chart_present-keyed guard only fires when chartRef is ALSO
// set, so adding chart_present to every well-formed HelmRelease cannot move the
// golden rc-154 edge count.
func TestFluxHelmReconcilesFromWellFormedChartWithChartPresentStillResolves(t *testing.T) {
	t.Parallel()

	helmRelease := projector.EntityRow{
		Label:      "FluxHelmRelease",
		EntityID:   "uid-hr",
		EntityName: "podinfo",
		FilePath:   "/repo/helmrelease.yaml",
		Metadata: map[string]any{
			"namespace":            "flux-system",
			"chart_present":        "true",
			"chart":                "podinfo",
			"source_ref_kind":      "HelmRepository",
			"source_ref_name":      "podinfo",
			"source_ref_namespace": "flux-system",
		},
	}

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			helmRelease,
			fluxSourceRowEntity("FluxHelmRepository", "uid-repo", "podinfo", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxHelmRepository {uid: row.target_uid}")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["target_uid"] != "uid-repo" {
		t.Fatalf("well-formed chart+sourceRef CR (with chart_present) must resolve: rows = %+v, want one row targeting uid-repo", rows)
	}
	if rows[0]["resolution_mode"] != "namespace_exact" {
		t.Fatalf("resolution_mode = %v, want namespace_exact", rows[0]["resolution_mode"])
	}
}

// TestFluxHelmReconcilesFromChartRefOnlyStillResolves guards against the
// chart_present fix over-suppressing the legitimate chartRef-only case: a
// well-formed HelmRelease with ONLY spec.chartRef (no spec.chart block at all,
// so chart_present is absent) must still resolve its chartRef edge.
func TestFluxHelmReconcilesFromChartRefOnlyStillResolves(t *testing.T) {
	t.Parallel()

	helmRelease := projector.EntityRow{
		Label:      "FluxHelmRelease",
		EntityID:   "uid-hr",
		EntityName: "helmrelease",
		FilePath:   "/repo/helmrelease.yaml",
		Metadata: map[string]any{
			"namespace":           "flux-system",
			"chart_ref_kind":      "OCIRepository",
			"chart_ref_name":      "podinfo-oci",
			"chart_ref_namespace": "flux-system",
		},
	}

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			helmRelease,
			fluxSourceRowEntity("FluxOCIRepository", "uid-oci", "podinfo-oci", "flux-system", "/repo/sources.yaml"),
		},
	}

	stmts := fluxReconcilesFromEdgeStatements(mat)
	merge := mergeStatementContaining(t, stmts, "FluxHelmRelease {uid: row.source_uid})\nMATCH (s:FluxOCIRepository")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["target_uid"] != "uid-oci" {
		t.Fatalf("chartRef-only CR should still resolve: rows = %+v, want one row targeting uid-oci", rows)
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
