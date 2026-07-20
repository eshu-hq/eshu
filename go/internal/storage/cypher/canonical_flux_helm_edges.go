// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fluxHelmSourceRefKindToLabel maps a Flux HelmRelease's
// spec.chart.spec.sourceRef.kind to the typed graph label PR A/C1 registered
// for it. Mirrors fluxSourceRefKindToLabel (canonical_flux_edges.go) but adds
// HelmRepository, a source kind a Kustomization's sourceRef never names. A
// kind absent from this map is an honest non-link: the HelmRelease is
// excluded from resolution via this path entirely rather than guessed at.
var fluxHelmSourceRefKindToLabel = map[string]string{
	"HelmRepository": "FluxHelmRepository",
	"GitRepository":  "FluxGitRepository",
	"Bucket":         "FluxBucket",
}

// fluxHelmChartRefKindToLabel maps a Flux HelmRelease's spec.chartRef.kind to
// the typed graph label it resolves against. HelmChart is DELIBERATELY
// ABSENT from this map: Eshu's existing HelmChart label models a Chart.yaml
// DIRECTORY ((name,path) identity, schema_tables.go), NOT the Flux HelmChart
// custom resource -- linking chartRef.kind=HelmChart to that label would be a
// fabricated cross-class join between two graph identities that happen to
// share a name. A HelmRelease whose chartRef names a HelmChart CR is an
// honest non-link, exactly like an unmapped sourceRef.kind.
var fluxHelmChartRefKindToLabel = map[string]string{
	"OCIRepository": "FluxOCIRepository",
}

// retractFluxHelmReconcilesFromEdgesCypher deletes stale RECONCILES_FROM
// edges from this materialization's FluxHelmRelease source nodes, the
// FluxHelmRelease-anchored sibling of retractFluxReconcilesFromEdgesCypher
// (canonical_flux_edges.go). Emitted with Drain=true FROM THE FIRST COMMIT of
// this edge (unlike the Kustomization-anchored retract, which started
// Drain=false and was fixed later): the same NornicDB grouped-write DELETE
// no-op (#4476) applies here, so there is no reason to repeat the bug before
// fixing it.
const retractFluxHelmReconcilesFromEdgesCypher = `UNWIND $source_uids AS uid
MATCH (h:FluxHelmRelease {uid: uid})-[r:RECONCILES_FROM]->()
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`

// canonicalNodeFluxHelmReconcilesFromHelmRepositoryEdgeCypher links a
// FluxHelmRelease to the FluxHelmRepository its spec.chart.spec.sourceRef (or
// spec.chartRef, though chartRef never names a HelmRepository per
// fluxHelmChartRefKindToLabel) resolved against. reconciler_kind and via are
// literal: this template is reached ONLY via the chart_source_ref path (a
// HelmRepository can never be a chartRef target), so via is always
// 'chart_source_ref' for every row this template ever receives.
const canonicalNodeFluxHelmReconcilesFromHelmRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (h:FluxHelmRelease {uid: row.source_uid})
MATCH (s:FluxHelmRepository {uid: row.target_uid})
MERGE (h)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'HelmRelease',
    r.via = 'chart_source_ref'`

// canonicalNodeFluxHelmReconcilesFromGitRepositoryEdgeCypher is the
// GitRepository target-label sibling, reached via the chart_source_ref path
// (spec.chart.spec.sourceRef naming a GitRepository).
const canonicalNodeFluxHelmReconcilesFromGitRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (h:FluxHelmRelease {uid: row.source_uid})
MATCH (s:FluxGitRepository {uid: row.target_uid})
MERGE (h)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'HelmRelease',
    r.via = 'chart_source_ref'`

// canonicalNodeFluxHelmReconcilesFromOCIRepositoryEdgeCypher is the
// OCIRepository target-label template, reached ONLY via the chart_ref path
// (spec.chartRef naming an OCIRepository -- fluxHelmChartRefKindToLabel is
// the only source of an OCIRepository target for a HelmRelease), so via is
// always 'chart_ref' for every row this template ever receives.
const canonicalNodeFluxHelmReconcilesFromOCIRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (h:FluxHelmRelease {uid: row.source_uid})
MATCH (s:FluxOCIRepository {uid: row.target_uid})
MERGE (h)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'HelmRelease',
    r.via = 'chart_ref'`

// canonicalNodeFluxHelmReconcilesFromBucketEdgeCypher is the Bucket
// target-label sibling, reached via the chart_source_ref path.
const canonicalNodeFluxHelmReconcilesFromBucketEdgeCypher = `UNWIND $rows AS row
MATCH (h:FluxHelmRelease {uid: row.source_uid})
MATCH (s:FluxBucket {uid: row.target_uid})
MERGE (h)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'HelmRelease',
    r.via = 'chart_source_ref'`

// fluxHelmReconcilesFromCypherByLabel selects among the four pre-written
// static Cypher templates by target label, the FluxHelmRelease-anchored
// sibling of fluxReconcilesFromCypherByLabel. Kustomization -> FluxHelmRepository
// is NOT a valid pair (a Kustomization's sourceRef never names a
// HelmRepository, see fluxSourceRefKindToLabel), so no such template exists.
var fluxHelmReconcilesFromCypherByLabel = map[string]string{
	"FluxHelmRepository": canonicalNodeFluxHelmReconcilesFromHelmRepositoryEdgeCypher,
	"FluxGitRepository":  canonicalNodeFluxHelmReconcilesFromGitRepositoryEdgeCypher,
	"FluxOCIRepository":  canonicalNodeFluxHelmReconcilesFromOCIRepositoryEdgeCypher,
	"FluxBucket":         canonicalNodeFluxHelmReconcilesFromBucketEdgeCypher,
}

// collectFluxHelmReleaseEntities extracts every FluxHelmRelease entity's uid
// (for the retract scope, regardless of resolvability) and the subset with a
// resolvable, valid reference for resolution.
//
// Per the Flux HelmRelease API, exactly one of spec.chart or spec.chartRef
// must be set. This function enforces that as an honest-non-link rule using
// the signals the parser actually emits: the presence of a spec.chart block
// (a non-empty `chart` field, or a non-empty source_ref_name from
// spec.chart.spec.sourceRef.name) and a non-empty chart_ref_kind/name (from
// spec.chartRef). A chart block AND a chartRef both present -- an invalid CR
// Flux rejects at admission, so it never reconciles -- produces no entity,
// never an edge for a CR that can never reconcile (issue #5483 C1 P3-1: the
// guard keys on the chart BLOCK, not only a resolvable sourceRef name, so a
// doubly-malformed CR with a chart block but no sourceRef plus a chartRef is
// still an honest non-link). Neither present -- an incomplete CR, or a
// HelmRelease with no reference field the parser captured -- also produces no
// entity: there is nothing to resolve against.
func collectFluxHelmReleaseEntities(entities []projector.EntityRow) ([]fluxReconcilerEntity, []string) {
	var helmReleases []fluxReconcilerEntity
	var uids []string
	for _, entity := range entities {
		if entity.Label != "FluxHelmRelease" {
			continue
		}
		if entity.EntityID != "" {
			uids = append(uids, entity.EntityID)
		}

		chart := metadataString(entity.Metadata, "chart")
		sourceRefKind := metadataString(entity.Metadata, "source_ref_kind")
		sourceRefName := metadataString(entity.Metadata, "source_ref_name")
		sourceRefNamespace := metadataString(entity.Metadata, "source_ref_namespace")
		chartRefKind := metadataString(entity.Metadata, "chart_ref_kind")
		chartRefName := metadataString(entity.Metadata, "chart_ref_name")
		chartRefNamespace := metadataString(entity.Metadata, "chart_ref_namespace")

		// A spec.chart block is present when either the chart name or its
		// nested sourceRef.name was captured -- key the mutual-exclusion guard
		// on the block, not just a resolvable sourceRef name, so a chart block
		// with no sourceRef plus a chartRef is still caught (P3-1).
		hasChartBlock := chart != "" || sourceRefName != ""
		hasChartRef := chartRefKind != "" || chartRefName != ""
		if hasChartBlock && hasChartRef {
			// Both spec.chart and spec.chartRef set: an invalid CR per the
			// Flux API (Flux rejects it at admission, so it never
			// reconciles). Honest non-link, never a fabricated pick or an
			// edge for a CR that can never reconcile.
			continue
		}

		var refKind, refName, refNamespace, targetLabel string
		switch {
		case hasChartRef:
			refKind, refName, refNamespace = chartRefKind, chartRefName, chartRefNamespace
			if refName == "" {
				continue // no chartRef name: never resolvable
			}
			label, ok := fluxHelmChartRefKindToLabel[refKind]
			if !ok {
				// Unmapped chartRef.kind (HelmChart, or absent/unrecognized):
				// honest non-link.
				continue
			}
			targetLabel = label
		case sourceRefName != "":
			refKind, refName, refNamespace = sourceRefKind, sourceRefName, sourceRefNamespace
			label, ok := fluxHelmSourceRefKindToLabel[refKind]
			if !ok {
				// Absent or unknown sourceRef.kind: honest non-link.
				continue
			}
			targetLabel = label
		default:
			// A chart block with no resolvable sourceRef name, or neither
			// reference field set: nothing to resolve against.
			continue
		}

		helmReleases = append(helmReleases, fluxReconcilerEntity{
			uid:          entity.EntityID,
			filePath:     entity.FilePath,
			namespace:    metadataString(entity.Metadata, "namespace"),
			sourceLabel:  "FluxHelmRelease",
			targetLabel:  targetLabel,
			refKind:      refKind,
			refName:      refName,
			refNamespace: refNamespace,
		})
	}
	return helmReleases, uids
}
