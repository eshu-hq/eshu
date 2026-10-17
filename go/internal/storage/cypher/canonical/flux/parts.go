// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package flux

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// Part is one leaf-local edge statement part: the static Cypher template,
// its parameters, and the two routing flags the parent cypher package maps
// onto Statement{Operation, Drain}. Part carries no Statement so this leaf
// never imports its parent (root phases file is the caller).
type Part struct {
	// Cypher is the static template to execute.
	Cypher string
	// Parameters are the statement parameters.
	Parameters map[string]any
	// IsRetract marks a stale-edge retraction (parent maps to
	// OperationCanonicalRetract); false marks an upsert.
	IsRetract bool
	// Drain marks a retract the NornicDB phase-group executor must run as
	// a standalone autocommit statement (parent maps to Statement.Drain).
	Drain bool
}

// fluxReconcilesFromWriteReasons is the write-reason registry
// MaterializedEdgeTypes exposes to the blast-radius
// edge-materialization coverage registry
// (go/internal/query/querycontract/edge_materialization_coverage.go),
// mirroring crossplaneSatisfiedByWriteReasons.
var fluxReconcilesFromWriteReasons = map[string]string{
	"RECONCILES_FROM": "FluxKustomization spec.sourceRef or FluxHelmRelease spec.chart.spec.sourceRef/spec.chartRef resolved against exactly one source CR (FluxGitRepository/FluxOCIRepository/FluxBucket/FluxHelmRepository) via the shared deterministic same-repo namespace/name tiers (T1-T4); never fabricated across a namespace mismatch, dangling ref, unresolved ambiguity, an unmapped ref kind (chartRef kind HelmChart is deliberately never linked -- it names a different graph label, the Chart.yaml directory), or an invalid both-chart-and-chartRef HelmRelease",
}

// MaterializedEdgeTypes returns a defensive copy of
// fluxReconcilesFromWriteReasons: the graph relationship types the Flux
// canonical edge writer actually accepts, mapped to the write reason recorded
// on each MERGEd edge. Mirrors CrossplaneRelationshipMaterializedEdgeTypes.
//
// It returns a copy so a caller cannot mutate the package's source of truth.
func MaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(fluxReconcilesFromWriteReasons))
	for edgeType, reason := range fluxReconcilesFromWriteReasons {
		out[edgeType] = reason
	}
	return out
}

// EdgeParts returns the RECONCILES_FROM edge statement parts for the
// FluxKustomization and FluxHelmRelease entities in the materialization
// (issue #5360 PR B, extended by issue #5483 C1), or nil when there are
// neither so the statements never run for non-Flux repos. Both reconciler
// kinds resolve through the IDENTICAL T1-T4 tiers
// (resolveFluxSourceCandidate) but are retract-scoped and Cypher-templated
// separately, since they anchor on different source labels. Edges are
// resolved in Go (resolveFluxReconciliationRows) and matched by canonical
// uid, mirroring atlantisEdgeStatements.
//
// Parts are Statement-free: the parent cypher package wraps each part into
// a Statement{Operation, Drain}. Retract parts precede upsert parts.
func EdgeParts(mat canonical.CanonicalMaterialization) []Part {
	kustomizations, allKustomizationUIDs := collectFluxKustomizationEntities(mat.Entities)
	helmReleases, allHelmReleaseUIDs := collectFluxHelmReleaseEntities(mat.Entities)
	if len(allKustomizationUIDs) == 0 && len(allHelmReleaseUIDs) == 0 {
		return nil
	}

	candidatesByKey := collectFluxSourceEntities(mat.Entities)
	reconcilers := make([]fluxReconcilerEntity, 0, len(kustomizations)+len(helmReleases))
	reconcilers = append(reconcilers, kustomizations...)
	reconcilers = append(reconcilers, helmReleases...)
	rows := resolveFluxReconciliationRows(reconcilers, candidatesByKey)

	rowsByLabelPair := map[string]map[string][]map[string]any{
		"FluxKustomization": {},
		"FluxHelmRelease":   {},
	}
	for _, row := range rows {
		byLabel := rowsByLabelPair[row.sourceLabel]
		byLabel[row.targetLabel] = append(byLabel[row.targetLabel], map[string]any{
			"source_uid":           row.sourceUID,
			"target_uid":           row.targetUID,
			"generation_id":        mat.GenerationID,
			"resolution_mode":      row.resolutionMode,
			"source_ref_kind":      row.sourceRefKind,
			"source_ref_name":      row.sourceRefName,
			"source_ref_namespace": nilIfEmptyString(row.sourceRefNamespace),
			"namespace_defaulted":  row.namespaceDefaulted,
		})
	}

	var parts []Part
	if !mat.FirstGeneration {
		if len(allKustomizationUIDs) > 0 {
			parts = append(parts, Part{
				Cypher: retractFluxReconcilesFromEdgesCypher,
				Parameters: map[string]any{
					"source_uids":   allKustomizationUIDs,
					"generation_id": mat.GenerationID,
				},
				IsRetract: true,
				Drain:     true,
			})
		}
		if len(allHelmReleaseUIDs) > 0 {
			parts = append(parts, Part{
				Cypher: retractFluxHelmReconcilesFromEdgesCypher,
				Parameters: map[string]any{
					"source_uids":   allHelmReleaseUIDs,
					"generation_id": mat.GenerationID,
				},
				IsRetract: true,
				Drain:     true,
			})
		}
	}
	for _, targetLabel := range []string{"FluxGitRepository", "FluxOCIRepository", "FluxBucket"} {
		labelRows := rowsByLabelPair["FluxKustomization"][targetLabel]
		if len(labelRows) == 0 {
			continue
		}
		parts = append(parts, Part{
			Cypher:     fluxReconcilesFromCypherByLabel[targetLabel],
			Parameters: map[string]any{"rows": labelRows},
		})
	}
	for _, targetLabel := range []string{"FluxHelmRepository", "FluxGitRepository", "FluxOCIRepository", "FluxBucket"} {
		labelRows := rowsByLabelPair["FluxHelmRelease"][targetLabel]
		if len(labelRows) == 0 {
			continue
		}
		parts = append(parts, Part{
			Cypher:     fluxHelmReconcilesFromCypherByLabel[targetLabel],
			Parameters: map[string]any{"rows": labelRows},
		})
	}
	return parts
}
