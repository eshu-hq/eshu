// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// helmTemplateValueReferenceEvidenceKind isolates the Helm template-value
// REFERENCES edge from the code-symbol REFERENCES edges that share the edge
// type. The B-7 golden corpus gate filters rc-35 on this kind so the assertion
// is provably zero without the Helm template fixture (no other flow stamps it).
const helmTemplateValueReferenceEvidenceKind = "HELM_TEMPLATE_VALUE_REFERENCE"

// canonicalNodeHelmTemplateValueReferenceEdgeCypher links a Helm template value
// usage (`{{ .Values.<path> }}` in a templates/*.yaml manifest) to the
// values.yaml leaf definition it reads. Source and target are matched by their
// canonical uid (supplied per row from Go), mirroring the Atlantis MANAGES /
// GitLab DEFINES_JOB structural edges.
//
// The edge uses a DEDICATED HELM_VALUE_REFERENCE relationship type rather than
// the generic REFERENCES type. This is a performance requirement, not a naming
// preference: on NornicDB a relationship DELETE scales with the size of the
// relationship-TYPE index, so retracting stale Helm edges against the shared
// REFERENCES type (which also holds every code-symbol call, ~52k at repo scale)
// is O(all-REFERENCES) per delete and blows the canonical-write budget (#4464,
// #4476). A dedicated type keeps the retract's delete-index tiny (only Helm
// value edges) so the retract is fast. evidence_kinds is retained so the rc-35
// gate can still isolate the Helm verb by evidence, and it is also semantically
// truer: a Helm .Values reference is not a code symbol reference.
const canonicalNodeHelmTemplateValueReferenceEdgeCypher = `UNWIND $rows AS row
MATCH (u:HelmTemplateValueUsage {uid: row.source_uid})
MATCH (d:HelmValueDefinition {uid: row.target_uid})
MERGE (u)-[r:HELM_VALUE_REFERENCE]->(d)
SET r.evidence_source = 'projector/canonical',
    r.generation_id = row.generation_id,
    r.evidence_kinds = row.evidence_kinds,
    r.source_tool = row.source_tool,
    r.reason = 'Helm template reads a values.yaml definition via .Values',
    r.call_kind = 'helm_template_value_reference'`

// retractHelmTemplateValueReferenceEdgesCypher deletes stale Helm template-value
// HELM_VALUE_REFERENCE edges from this materialization's HelmTemplateValueUsage
// source nodes. The edge is MERGE-only between surviving nodes, so neither
// repository_cleanup (DETACH DELETE of the Repository node) nor entity_retract
// (edges of DELETED nodes only) removes a stale edge when both endpoints survive
// into the current generation but the value reference changed. The retract is
// scoped by the HelmTemplateValueUsage source label and is generation-guarded so
// the subsequent MERGE re-writes the current edge. Because HELM_VALUE_REFERENCE
// is a dedicated type, this DELETE only touches the small Helm value edge
// population, never the large shared REFERENCES index (#4476). The writer marks
// this statement Drain so the NornicDB phase-group executor runs it as a
// standalone autocommit statement: an UNWIND relationship DELETE inside the
// grouped ExecuteWrite transaction silently no-ops on commit, so it must run
// autocommit to actually persist the deletes.
const retractHelmTemplateValueReferenceEdgesCypher = `UNWIND $source_uids AS uid
MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:HELM_VALUE_REFERENCE]->(:HelmValueDefinition)
WHERE r.evidence_source = 'projector/canonical'
  AND r.call_kind = 'helm_template_value_reference'
  AND r.generation_id <> $generation_id
DELETE r`

// retractLegacyHelmReferenceEdgesCypher removes Helm template-value edges that a
// pre-#4476 release wrote on the SHARED REFERENCES type (before this edge moved
// to the dedicated HELM_VALUE_REFERENCE type). On an in-place upgrade those old
// edges survive: entity_retract does not remove them because both endpoint nodes
// are re-upserted into the current generation, and the new-type retract only
// touches HELM_VALUE_REFERENCE. This one-time migration deletes every REFERENCES
// edge carrying the helm_template_value_reference call_kind (all of which are
// legacy — the current writer never emits that verb on the REFERENCES type), so
// upgraded installations converge to the dedicated type without a clean rebuild.
// It is scoped to this materialization's usage source uids (indexed seed) and,
// once the legacy edges are gone, matches nothing and is cheap. Not
// generation-guarded: every such edge is legacy and must be removed regardless
// of generation. Drain-marked so it runs autocommit (an UNWIND relationship
// DELETE no-ops inside the grouped ExecuteWrite transaction, #4476).
const retractLegacyHelmReferenceEdgesCypher = `UNWIND $source_uids AS uid
MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:REFERENCES]->(:HelmValueDefinition)
WHERE r.call_kind = 'helm_template_value_reference'
DELETE r`

// helmTemplateValueEntity is one Helm template-value content entity (a usage or a
// definition) reduced to the fields the REFERENCES edge needs.
type helmTemplateValueEntity struct {
	uid      string
	name     string
	chartDir string
}

// helmTemplateValueEdgeStatements returns the REFERENCES edge statements linking
// each HelmTemplateValueUsage to the HelmValueDefinition with the same dotted
// path in the same chart, or nil when there are none so the statement never runs
// for non-Helm repos. Resolution is scoped per chart: a usage in
// `<chart>/templates/*.yaml` resolves only against definitions in
// `<chart>/values.yaml`, so two charts that both define `image.tag` do not
// cross-link. Edges are resolved in Go and matched by uid, which is robust where
// bound-variable property matching is not.
func helmTemplateValueEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	var usages []helmTemplateValueEntity
	// definitionUIDByChartAndName: "<chartDir>\x00<dotted.path>" -> definition uid.
	definitionUIDByChartAndName := make(map[string]string)

	for _, entity := range mat.Entities {
		switch entity.Label {
		case "HelmTemplateValueUsage":
			usages = append(usages, helmTemplateValueEntity{
				uid:      entity.EntityID,
				name:     entity.EntityName,
				chartDir: helmUsageChartDir(entity.FilePath),
			})
		case "HelmValueDefinition":
			chartDir := helmDefinitionChartDir(entity.FilePath)
			definitionUIDByChartAndName[chartDir+"\x00"+entity.EntityName] = entity.EntityID
		}
	}

	if len(usages) == 0 || len(definitionUIDByChartAndName) == 0 {
		return nil
	}

	// Deterministic iteration for stable batch ordering.
	sort.Slice(usages, func(i, j int) bool {
		if usages[i].uid != usages[j].uid {
			return usages[i].uid < usages[j].uid
		}
		return usages[i].name < usages[j].name
	})

	var rows []map[string]any
	for _, usage := range usages {
		targetUID, ok := definitionUIDByChartAndName[usage.chartDir+"\x00"+usage.name]
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{
			"source_uid":     usage.uid,
			"target_uid":     targetUID,
			"generation_id":  mat.GenerationID,
			"evidence_kinds": []string{helmTemplateValueReferenceEvidenceKind},
			"source_tool":    "helm",
		})
	}

	if len(rows) == 0 {
		return nil
	}

	var stmts []Statement
	// Retract stale edges BEFORE the MERGE so a re-projection with a changed
	// .Values usage set, where both endpoint nodes survive into the current
	// generation, drops the old generation's edge that repository_cleanup and
	// entity_retract leave behind. Scoped to THIS materialization's usage source
	// uids and only touching projector/canonical helm_template_value_reference
	// edges of a prior generation. Statement order within a phase is preserved by
	// the writer, so emitting the retract first guarantees it executes before the
	// MERGE in the same structural_edges phase.
	if sourceUIDs := helmTemplateValueSourceUIDs(rows); len(sourceUIDs) > 0 {
		// One-time upgrade migration: drop any legacy Helm edges written on the
		// shared REFERENCES type by a pre-#4476 release, before the new-type
		// retract/MERGE. Drain-marked (autocommit) and emitted first so it runs
		// before the dedicated-type writes in the same phase; matches nothing
		// once an installation has converged to HELM_VALUE_REFERENCE.
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractLegacyHelmReferenceEdgesCypher,
			Parameters: map[string]any{
				"source_uids": sourceUIDs,
			},
			Drain: true,
		})
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractHelmTemplateValueReferenceEdgesCypher,
			Parameters: map[string]any{
				"source_uids":   sourceUIDs,
				"generation_id": mat.GenerationID,
			},
			// Drain-mark this relationship retract so the NornicDB phase-group
			// executor runs it as a standalone autocommit statement rather than
			// inside the grouped structural-edges ExecuteWrite transaction. An
			// UNWIND relationship DELETE inside an explicit multi-statement
			// transaction selects and buffers the deletes but they do not survive
			// COMMIT (a NornicDB transaction-layer limitation, #4476); autocommit
			// deletes correctly. The dedicated HELM_VALUE_REFERENCE type keeps the
			// delete-index small, so a single autocommit DELETE is fast and no
			// LIMIT drain loop is needed for the realistic Helm value edge count.
			Drain: true,
		})
	}

	stmts = append(stmts, Statement{
		Operation:  OperationCanonicalUpsert,
		Cypher:     canonicalNodeHelmTemplateValueReferenceEdgeCypher,
		Parameters: map[string]any{"rows": rows},
	})
	return stmts
}

// helmTemplateValueSourceUIDs returns the distinct REFERENCES source (usage) uids
// from the resolved edge rows, so the retract is scoped to exactly the usages
// this materialization re-projects.
func helmTemplateValueSourceUIDs(rows []map[string]any) []string {
	seen := make(map[string]struct{}, len(rows))
	uids := make([]string, 0, len(rows))
	for _, row := range rows {
		uid, _ := row["source_uid"].(string)
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		uids = append(uids, uid)
	}
	return uids
}

// helmUsageChartDir returns the chart root directory for a template manifest
// path: the parent of the `templates/` directory the manifest lives in
// (`<chart>/templates/deployment.yaml` -> `<chart>`). When the path has no
// `templates` segment it falls back to the file's directory so a malformed
// layout still resolves within itself rather than silently cross-linking.
func helmUsageChartDir(filePath string) string {
	// The chart root is everything before the `templates` segment, so a nested
	// manifest (<chart>/templates/config/cm.yaml) resolves to <chart> — the same
	// chart root as <chart>/values.yaml — not an intermediate directory. This
	// agrees with isHelmTemplateManifest's chart-root resolution in the parser.
	parts := strings.Split(filePath, "/")
	for i, part := range parts {
		if part == "templates" && i > 0 {
			return strings.Join(parts[:i], "/")
		}
	}
	return path.Dir(filePath)
}

// helmDefinitionChartDir returns the chart root directory for a values.yaml
// path: the directory the values file lives in
// (`<chart>/values.yaml` -> `<chart>`).
func helmDefinitionChartDir(filePath string) string {
	return path.Dir(filePath)
}
