// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher links a
// FluxKustomization to the FluxGitRepository its spec.sourceRef resolved
// against. Both endpoints are matched by their canonical uid, resolved in Go
// (fluxReconcilesFromEdgeStatements), mirroring the Atlantis MANAGES/
// USES_WORKFLOW edges (canonical_atlantis_edges.go). One static template per
// target label -- never a data-driven label -- keeps the emitted Cypher fully
// pinned for the query-plan gate.
const canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (k:FluxKustomization {uid: row.source_uid})
MATCH (s:FluxGitRepository {uid: row.target_uid})
MERGE (k)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'Kustomization'`

// canonicalNodeFluxReconcilesFromOCIRepositoryEdgeCypher is the OCIRepository
// target-label sibling of canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher.
const canonicalNodeFluxReconcilesFromOCIRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (k:FluxKustomization {uid: row.source_uid})
MATCH (s:FluxOCIRepository {uid: row.target_uid})
MERGE (k)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'Kustomization'`

// canonicalNodeFluxReconcilesFromBucketEdgeCypher is the Bucket target-label
// sibling of canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher.
const canonicalNodeFluxReconcilesFromBucketEdgeCypher = `UNWIND $rows AS row
MATCH (k:FluxKustomization {uid: row.source_uid})
MATCH (s:FluxBucket {uid: row.target_uid})
MERGE (k)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted, r.reconciler_kind = 'Kustomization'`

// retractFluxReconcilesFromEdgesCypher deletes stale RECONCILES_FROM edges
// from this materialization's FluxKustomization source nodes. Unlike the
// Atlantis triad (three distinct relationship types), RECONCILES_FROM is one
// relationship type that can target any of three labels, so a single retract
// keyed on the relationship type (never scoped to a target label) covers all
// three -- matching the MATCH-then-anonymous-target shape, no label needed to
// select the stale edge. Emitted with Drain=true (see
// fluxReconcilesFromEdgeStatements): this is an UNWIND relationship DELETE
// inside the mixed structural_edges phase, and on NornicDB such a DELETE
// silently no-ops when it runs inside the phase's grouped ExecuteWrite
// transaction alongside the sibling RECONCILES_FROM MERGE upserts (#4476, the
// same class the Atlantis, Helm, and GitLab structural edges already guard
// against). The NornicDB phase-group executor runs a Drain-marked statement as
// its own standalone autocommit statement before the grouped upserts.
const retractFluxReconcilesFromEdgesCypher = `UNWIND $source_uids AS uid
MATCH (k:FluxKustomization {uid: uid})-[r:RECONCILES_FROM]->()
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`

// fluxReconcilesFromWriteReasons is the write-reason registry
// FluxRelationshipMaterializedEdgeTypes exposes to the blast-radius
// edge-materialization coverage registry
// (go/internal/query/edge_materialization_coverage.go), mirroring
// crossplaneSatisfiedByWriteReasons.
var fluxReconcilesFromWriteReasons = map[string]string{
	"RECONCILES_FROM": "FluxKustomization spec.sourceRef or FluxHelmRelease spec.chart.spec.sourceRef/spec.chartRef resolved against exactly one source CR (FluxGitRepository/FluxOCIRepository/FluxBucket/FluxHelmRepository) via the shared deterministic same-repo namespace/name tiers (T1-T4); never fabricated across a namespace mismatch, dangling ref, unresolved ambiguity, an unmapped ref kind (chartRef kind HelmChart is deliberately never linked -- it names a different graph label, the Chart.yaml directory), or an invalid both-chart-and-chartRef HelmRelease",
}

// FluxRelationshipMaterializedEdgeTypes returns a defensive copy of
// fluxReconcilesFromWriteReasons: the graph relationship types the Flux
// canonical edge writer actually accepts, mapped to the write reason recorded
// on each MERGEd edge. Mirrors CrossplaneRelationshipMaterializedEdgeTypes.
func FluxRelationshipMaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(fluxReconcilesFromWriteReasons))
	for edgeType, reason := range fluxReconcilesFromWriteReasons {
		out[edgeType] = reason
	}
	return out
}

// fluxSourceRefKindToLabel maps a Flux Kustomization's spec.sourceRef.kind to
// the typed graph label PR A registered for it. A kind absent from this map
// (e.g. the feature-gated ExternalArtifact) is an honest non-link: the
// Kustomization is excluded from resolution entirely rather than guessed at.
var fluxSourceRefKindToLabel = map[string]string{
	"GitRepository": "FluxGitRepository",
	"OCIRepository": "FluxOCIRepository",
	"Bucket":        "FluxBucket",
}

// fluxSourceLabels is the set of typed Flux source-CR labels a Kustomization's
// sourceRef or a HelmRelease's chart.spec.sourceRef/chartRef can resolve
// against. FluxHelmRepository was added by issue #5483 C1 (HelmRelease's
// sourceRef can name a HelmRepository, which a Kustomization's sourceRef never
// does).
var fluxSourceLabels = map[string]struct{}{
	"FluxGitRepository":  {},
	"FluxOCIRepository":  {},
	"FluxBucket":         {},
	"FluxHelmRepository": {},
}

// fluxReconcilerEntity is one Flux reconciler CR (FluxKustomization or
// FluxHelmRelease, issue #5483 C1) reduced to the fields the shared
// RECONCILES_FROM resolution (resolveFluxSourceCandidate, T1-T4) needs.
// FluxKustomization and FluxHelmRelease resolve against their source CR
// through the IDENTICAL tier algorithm; sourceLabel records which reconciler
// kind produced this entity, purely for template-family selection. The
// reconciler_kind and via edge properties are literal (not row-parameterized)
// in every static Cypher template, since sourceLabel+targetLabel already
// determine them uniquely for the current closed kind maps (see
// fluxHelmChartRefKindToLabel / fluxHelmSourceRefKindToLabel in
// canonical_flux_helm_edges.go), so no `via` field is threaded through the
// row.
type fluxReconcilerEntity struct {
	uid          string
	filePath     string
	namespace    string // the reconciler's own metadata.namespace, may be ""
	sourceLabel  string // "FluxKustomization" | "FluxHelmRelease"
	targetLabel  string // resolved target label, already mapped by the entity's own collector via its own closed kind map
	refKind      string // raw ref kind (spec.sourceRef.kind or spec.chartRef.kind)
	refName      string // raw ref name
	refNamespace string // raw ref namespace as declared, may be ""
}

// fluxSourceEntity is one FluxGitRepository/FluxOCIRepository/FluxBucket
// content entity reduced to the fields sourceRef resolution needs.
type fluxSourceEntity struct {
	uid       string
	label     string
	namespace string // declared metadata.namespace, may be "" (absent)
	filePath  string
}

// fluxReconciliationRow is one resolved FluxKustomization/FluxHelmRelease ->
// source-CR edge, ready to be grouped by (sourceLabel, targetLabel) into a
// per-template MERGE statement.
type fluxReconciliationRow struct {
	sourceUID          string
	targetUID          string
	sourceLabel        string // "FluxKustomization" | "FluxHelmRelease" -- selects the Cypher template family
	targetLabel        string
	resolutionMode     string
	sourceRefKind      string
	sourceRefName      string
	sourceRefNamespace string // the EFFECTIVE namespace used to resolve; "" means unknown (omitted on the edge)
	namespaceDefaulted bool
}

// fluxReconcilesFromEdgeStatements returns the RECONCILES_FROM edge statements
// for the FluxKustomization and FluxHelmRelease entities in the
// materialization (issue #5360 PR B, extended by issue #5483 C1), or nil when
// there are neither so the statements never run for non-Flux repos. Both
// reconciler kinds resolve through the IDENTICAL T1-T4 tiers
// (resolveFluxSourceCandidate) but are retract-scoped and Cypher-templated
// separately, since they anchor on different source labels. Edges are
// resolved in Go (resolveFluxReconciliationRows) and matched by canonical
// uid, mirroring atlantisEdgeStatements.
func fluxReconcilesFromEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
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

	var stmts []Statement
	if !mat.FirstGeneration {
		if len(allKustomizationUIDs) > 0 {
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractFluxReconcilesFromEdgesCypher,
				Parameters: map[string]any{
					"source_uids":   allKustomizationUIDs,
					"generation_id": mat.GenerationID,
				},
				Drain: true,
			})
		}
		if len(allHelmReleaseUIDs) > 0 {
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractFluxHelmReconcilesFromEdgesCypher,
				Parameters: map[string]any{
					"source_uids":   allHelmReleaseUIDs,
					"generation_id": mat.GenerationID,
				},
				Drain: true,
			})
		}
	}
	for _, targetLabel := range []string{"FluxGitRepository", "FluxOCIRepository", "FluxBucket"} {
		labelRows := rowsByLabelPair["FluxKustomization"][targetLabel]
		if len(labelRows) == 0 {
			continue
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     fluxReconcilesFromCypherByLabel[targetLabel],
			Parameters: map[string]any{"rows": labelRows},
		})
	}
	for _, targetLabel := range []string{"FluxHelmRepository", "FluxGitRepository", "FluxOCIRepository", "FluxBucket"} {
		labelRows := rowsByLabelPair["FluxHelmRelease"][targetLabel]
		if len(labelRows) == 0 {
			continue
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     fluxHelmReconcilesFromCypherByLabel[targetLabel],
			Parameters: map[string]any{"rows": labelRows},
		})
	}
	return stmts
}

// fluxReconcilesFromCypherByLabel selects among the three pre-written static
// Cypher templates by target label. This is a lookup among fixed strings, not
// a label-interpolated template: the Cypher text for each label is fully
// static.
var fluxReconcilesFromCypherByLabel = map[string]string{
	"FluxGitRepository": canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher,
	"FluxOCIRepository": canonicalNodeFluxReconcilesFromOCIRepositoryEdgeCypher,
	"FluxBucket":        canonicalNodeFluxReconcilesFromBucketEdgeCypher,
}

// collectFluxKustomizationEntities extracts every FluxKustomization entity's
// uid (for the retract scope, regardless of resolvability) and the subset
// with a resolvable sourceRef (non-empty name, known kind) for resolution.
func collectFluxKustomizationEntities(entities []projector.EntityRow) ([]fluxReconcilerEntity, []string) {
	var kustomizations []fluxReconcilerEntity
	var uids []string
	for _, entity := range entities {
		if entity.Label != "FluxKustomization" {
			continue
		}
		if entity.EntityID != "" {
			uids = append(uids, entity.EntityID)
		}
		refKind := metadataString(entity.Metadata, "source_ref_kind")
		refName := metadataString(entity.Metadata, "source_ref_name")
		if refName == "" {
			// No sourceRef name: never resolvable, an honest non-link.
			continue
		}
		targetLabel, ok := fluxSourceRefKindToLabel[refKind]
		if !ok {
			// Absent or unknown kind (e.g. ExternalArtifact): honest non-link.
			continue
		}
		kustomizations = append(kustomizations, fluxReconcilerEntity{
			uid:          entity.EntityID,
			filePath:     entity.FilePath,
			namespace:    metadataString(entity.Metadata, "namespace"),
			sourceLabel:  "FluxKustomization",
			targetLabel:  targetLabel,
			refKind:      refKind,
			refName:      refName,
			refNamespace: metadataString(entity.Metadata, "source_ref_namespace"),
		})
	}
	return kustomizations, uids
}

// collectFluxSourceEntities groups FluxGitRepository/FluxOCIRepository/
// FluxBucket entities by "<label>\x00<name>". A source CR with an empty name
// (metadata.generateName instead of metadata.name) is never inserted -- it
// must never false-join a Kustomization whose sourceRef.name is also empty.
func collectFluxSourceEntities(entities []projector.EntityRow) map[string][]fluxSourceEntity {
	out := map[string][]fluxSourceEntity{}
	for _, entity := range entities {
		if _, ok := fluxSourceLabels[entity.Label]; !ok {
			continue
		}
		name := strings.TrimSpace(entity.EntityName)
		if name == "" {
			continue
		}
		key := entity.Label + "\x00" + name
		out[key] = append(out[key], fluxSourceEntity{
			uid:       entity.EntityID,
			label:     entity.Label,
			namespace: metadataString(entity.Metadata, "namespace"),
			filePath:  entity.FilePath,
		})
	}
	return out
}

// resolveFluxReconciliationRows applies the deterministic T1-T4 resolution
// tiers (issue #5360 PR B design, reused verbatim for FluxHelmRelease by
// issue #5483 C1) to every resolvable reconciler entity (FluxKustomization or
// FluxHelmRelease), producing one row per entity that reaches a unique,
// honest resolution. An entity's targetLabel is already resolved by its own
// collector (each reconciler kind maps its ref kind through a DIFFERENT
// closed kind-to-label map), so this loop is entirely reconciler-neutral: it
// only reads targetLabel/refName/refNamespace/namespace/filePath, never a
// reconciler-specific field. Entities that reach no unique resolution
// (dangling ref, declared-namespace mismatch, or an unresolved tie) produce
// no row -- an edge is never fabricated. Rows are sorted by (sourceUID,
// targetUID) for byte-stable output regardless of map iteration order
// upstream.
func resolveFluxReconciliationRows(reconcilers []fluxReconcilerEntity, candidatesByKey map[string][]fluxSourceEntity) []fluxReconciliationRow {
	var rows []fluxReconciliationRow
	for _, r := range reconcilers {
		candidates := candidatesByKey[r.targetLabel+"\x00"+r.refName]
		if len(candidates) == 0 {
			continue // dangling ref: no candidate exists at all
		}

		refNS := r.refNamespace
		namespaceDefaulted := false
		if refNS == "" {
			refNS = r.namespace
			namespaceDefaulted = true
		}

		winner, mode, ok := resolveFluxSourceCandidate(candidates, refNS, r.filePath)
		if !ok {
			continue
		}

		rows = append(rows, fluxReconciliationRow{
			sourceUID:          r.uid,
			targetUID:          winner.uid,
			sourceLabel:        r.sourceLabel,
			targetLabel:        r.targetLabel,
			resolutionMode:     mode,
			sourceRefKind:      r.refKind,
			sourceRefName:      r.refName,
			sourceRefNamespace: refNS,
			namespaceDefaulted: namespaceDefaulted,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].sourceUID != rows[j].sourceUID {
			return rows[i].sourceUID < rows[j].sourceUID
		}
		return rows[i].targetUID < rows[j].targetUID
	})
	return rows
}

// resolveFluxSourceCandidate applies tiers T1-T4 for one Kustomization against
// its (kind,name)-keyed candidate set. refNS is the effective namespace
// (already defaulted from the Kustomization's own namespace when the declared
// sourceRef.namespace was empty), which may itself be "" (fully unknown).
func resolveFluxSourceCandidate(candidates []fluxSourceEntity, refNS string, kFilePath string) (fluxSourceEntity, string, bool) {
	if refNS == "" {
		// T4: refNS unknown repo-wide -- only a unique (kind,name) candidate
		// resolves; 2+ is ambiguous, skip.
		if len(candidates) == 1 {
			return candidates[0], "name_unique_namespace_unknown", true
		}
		return fluxSourceEntity{}, "", false
	}

	var exact []fluxSourceEntity
	for _, c := range candidates {
		if c.namespace != "" && c.namespace == refNS {
			exact = append(exact, c)
		}
	}
	switch {
	case len(exact) == 1:
		// T1: namespace exact, unique.
		return exact[0], "namespace_exact", true
	case len(exact) >= 2:
		// T2: multi-cluster duplicate disambiguation.
		winner, ok := disambiguateFluxCandidates(exact, kFilePath)
		if !ok {
			return fluxSourceEntity{}, "", false
		}
		return winner, "namespace_exact_nearest_path", true
	}

	// Zero namespace-exact matches: T3 (absent-namespace candidate) if unique.
	var absent []fluxSourceEntity
	for _, c := range candidates {
		if c.namespace == "" {
			absent = append(absent, c)
		}
	}
	if len(absent) == 1 {
		return absent[0], "name_unique_namespace_unknown", true
	}
	// Declared-namespace mismatch on every candidate, or an ambiguous
	// absent-namespace set: no edge, ever.
	return fluxSourceEntity{}, "", false
}

// disambiguateFluxCandidates resolves a T2 multi-cluster duplicate: 2+
// candidates share (kind, name, namespace). It prefers a candidate declared in
// the SAME FILE as the Kustomization (the gotk-sync.yaml layout, where a
// cluster's Kustomization and its GitRepository live in one file); failing
// that, the unique candidate with the longest common directory prefix with
// the Kustomization's file path. A tie at the maximum (including 2+
// same-file candidates) is an honest ambiguity: skip, never a representative
// pick.
func disambiguateFluxCandidates(candidates []fluxSourceEntity, kFilePath string) (fluxSourceEntity, bool) {
	var sameFile []fluxSourceEntity
	for _, c := range candidates {
		if c.filePath == kFilePath {
			sameFile = append(sameFile, c)
		}
	}
	if len(sameFile) == 1 {
		return sameFile[0], true
	}
	if len(sameFile) >= 2 {
		return fluxSourceEntity{}, false
	}

	kDir := path.Dir(kFilePath)
	bestDepth := -1
	var winners []fluxSourceEntity
	for _, c := range candidates {
		depth := commonDirPrefixDepth(kDir, path.Dir(c.filePath))
		switch {
		case depth > bestDepth:
			bestDepth = depth
			winners = []fluxSourceEntity{c}
		case depth == bestDepth:
			winners = append(winners, c)
		}
	}
	if len(winners) == 1 {
		return winners[0], true
	}
	return fluxSourceEntity{}, false
}

// commonDirPrefixDepth counts the number of leading path segments two
// directory paths share.
func commonDirPrefixDepth(a, b string) int {
	as := strings.Split(strings.Trim(a, "/"), "/")
	bs := strings.Split(strings.Trim(b, "/"), "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

// nilIfEmptyString returns nil for an empty string, else the string itself.
// Used for row fields that must be OMITTED from the graph edge (never written
// as an empty string) when unknown: a Cypher `SET r.prop = null` removes the
// property entirely (Neo4j/NornicDB semantics), so passing nil through the
// row achieves "omitted when unknown" without a second per-row branch in the
// Cypher template.
func nilIfEmptyString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
