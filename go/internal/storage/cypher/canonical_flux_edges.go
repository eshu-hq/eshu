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
    r.namespace_defaulted = row.namespace_defaulted`

// canonicalNodeFluxReconcilesFromOCIRepositoryEdgeCypher is the OCIRepository
// target-label sibling of canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher.
const canonicalNodeFluxReconcilesFromOCIRepositoryEdgeCypher = `UNWIND $rows AS row
MATCH (k:FluxKustomization {uid: row.source_uid})
MATCH (s:FluxOCIRepository {uid: row.target_uid})
MERGE (k)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted`

// canonicalNodeFluxReconcilesFromBucketEdgeCypher is the Bucket target-label
// sibling of canonicalNodeFluxReconcilesFromGitRepositoryEdgeCypher.
const canonicalNodeFluxReconcilesFromBucketEdgeCypher = `UNWIND $rows AS row
MATCH (k:FluxKustomization {uid: row.source_uid})
MATCH (s:FluxBucket {uid: row.target_uid})
MERGE (k)-[r:RECONCILES_FROM]->(s)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.resolution_mode = row.resolution_mode, r.source_ref_kind = row.source_ref_kind,
    r.source_ref_name = row.source_ref_name, r.source_ref_namespace = row.source_ref_namespace,
    r.namespace_defaulted = row.namespace_defaulted`

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
	"RECONCILES_FROM": "FluxKustomization spec.sourceRef resolved against exactly one FluxGitRepository/FluxOCIRepository/FluxBucket source CR via deterministic same-repo namespace/name tiers (T1-T4); never fabricated across a namespace mismatch, dangling ref, or unresolved ambiguity",
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
// sourceRef can resolve against.
var fluxSourceLabels = map[string]struct{}{
	"FluxGitRepository": {},
	"FluxOCIRepository": {},
	"FluxBucket":        {},
}

// fluxKustomizationEntity is one FluxKustomization content entity reduced to
// the fields the RECONCILES_FROM resolution needs.
type fluxKustomizationEntity struct {
	uid          string
	filePath     string
	namespace    string // the Kustomization's own metadata.namespace, may be ""
	refKind      string // raw spec.sourceRef.kind
	refName      string // raw spec.sourceRef.name
	refNamespace string // raw spec.sourceRef.namespace as declared, may be ""
}

// fluxSourceEntity is one FluxGitRepository/FluxOCIRepository/FluxBucket
// content entity reduced to the fields sourceRef resolution needs.
type fluxSourceEntity struct {
	uid       string
	label     string
	namespace string // declared metadata.namespace, may be "" (absent)
	filePath  string
}

// fluxReconciliationRow is one resolved FluxKustomization -> source-CR edge,
// ready to be grouped by targetLabel into a per-label MERGE statement.
type fluxReconciliationRow struct {
	sourceUID          string
	targetUID          string
	targetLabel        string
	resolutionMode     string
	sourceRefKind      string
	sourceRefName      string
	sourceRefNamespace string // the EFFECTIVE namespace used to resolve; "" means unknown (omitted on the edge)
	namespaceDefaulted bool
}

// fluxReconcilesFromEdgeStatements returns the RECONCILES_FROM edge statements
// for the FluxKustomization entities in the materialization, or nil when there
// are none so the statements never run for non-Flux repos. Edges are resolved
// in Go (resolveFluxReconciliationRows) and matched by canonical uid, mirroring
// atlantisEdgeStatements.
func fluxReconcilesFromEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	kustomizations, allKustomizationUIDs := collectFluxKustomizationEntities(mat.Entities)
	if len(allKustomizationUIDs) == 0 {
		return nil
	}

	candidatesByKey := collectFluxSourceEntities(mat.Entities)
	rows := resolveFluxReconciliationRows(kustomizations, candidatesByKey)

	rowsByLabel := map[string][]map[string]any{}
	for _, row := range rows {
		rowsByLabel[row.targetLabel] = append(rowsByLabel[row.targetLabel], map[string]any{
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
	for _, targetLabel := range []string{"FluxGitRepository", "FluxOCIRepository", "FluxBucket"} {
		labelRows := rowsByLabel[targetLabel]
		if len(labelRows) == 0 {
			continue
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     fluxReconcilesFromCypherByLabel[targetLabel],
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
func collectFluxKustomizationEntities(entities []projector.EntityRow) ([]fluxKustomizationEntity, []string) {
	var kustomizations []fluxKustomizationEntity
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
		if _, ok := fluxSourceRefKindToLabel[refKind]; !ok {
			// Absent or unknown kind (e.g. ExternalArtifact): honest non-link.
			continue
		}
		kustomizations = append(kustomizations, fluxKustomizationEntity{
			uid:          entity.EntityID,
			filePath:     entity.FilePath,
			namespace:    metadataString(entity.Metadata, "namespace"),
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
// tiers (issue #5360 PR B design) to every resolvable Kustomization, producing
// one row per Kustomization that reaches a unique, honest resolution.
// Kustomizations that reach no unique resolution (dangling ref, declared-
// namespace mismatch, or an unresolved tie) produce no row -- an edge is
// never fabricated. Rows are sorted by (sourceUID, targetUID) for byte-stable
// output regardless of map iteration order upstream.
func resolveFluxReconciliationRows(kustomizations []fluxKustomizationEntity, candidatesByKey map[string][]fluxSourceEntity) []fluxReconciliationRow {
	var rows []fluxReconciliationRow
	for _, k := range kustomizations {
		targetLabel := fluxSourceRefKindToLabel[k.refKind]
		candidates := candidatesByKey[targetLabel+"\x00"+k.refName]
		if len(candidates) == 0 {
			continue // dangling ref: no candidate exists at all
		}

		refNS := k.refNamespace
		namespaceDefaulted := false
		if refNS == "" {
			refNS = k.namespace
			namespaceDefaulted = true
		}

		winner, mode, ok := resolveFluxSourceCandidate(candidates, refNS, k.filePath)
		if !ok {
			continue
		}

		rows = append(rows, fluxReconciliationRow{
			sourceUID:          k.uid,
			targetUID:          winner.uid,
			targetLabel:        targetLabel,
			resolutionMode:     mode,
			sourceRefKind:      k.refKind,
			sourceRefName:      k.refName,
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
