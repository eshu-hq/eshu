// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"path"
	"sort"

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
// GitLab DEFINES_JOB structural edges. The edge reuses the generic REFERENCES
// type (usage -> definition, the same semantic as a code symbol reference) and
// carries evidence_kinds so the rc-35 gate can isolate the Helm verb from
// code-symbol REFERENCES edges.
const canonicalNodeHelmTemplateValueReferenceEdgeCypher = `UNWIND $rows AS row
MATCH (u:HelmTemplateValueUsage {uid: row.source_uid})
MATCH (d:HelmValueDefinition {uid: row.target_uid})
MERGE (u)-[r:REFERENCES]->(d)
SET r.evidence_source = 'projector/canonical',
    r.generation_id = row.generation_id,
    r.evidence_kinds = row.evidence_kinds,
    r.reason = 'Helm template reads a values.yaml definition via .Values',
    r.call_kind = 'helm_template_value_reference'`

// retractHelmTemplateValueReferenceEdgesCypher deletes stale Helm template-value
// REFERENCES edges from this materialization's HelmTemplateValueUsage source
// nodes. REFERENCES is a MERGE-only edge between surviving nodes, so neither
// repository_cleanup (DETACH DELETE of the Repository node) nor entity_retract
// (edges of DELETED nodes only) removes a stale edge when both endpoints survive
// into the current generation but the value reference changed. The retract is
// scoped by the HelmTemplateValueUsage source label and the
// helm_template_value_reference call_kind so it NEVER touches the code-symbol
// REFERENCES edges that share the edge type, and is generation-guarded so the
// subsequent MERGE re-writes the current edge. Bounded by the .Values usage
// count in one chart's templates.
const retractHelmTemplateValueReferenceEdgesCypher = `UNWIND $source_uids AS uid
MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:REFERENCES]->(:HelmValueDefinition)
WHERE r.evidence_source = 'projector/canonical'
  AND r.call_kind = 'helm_template_value_reference'
  AND r.generation_id <> $generation_id
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
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractHelmTemplateValueReferenceEdgesCypher,
			Parameters: map[string]any{
				"source_uids":   sourceUIDs,
				"generation_id": mat.GenerationID,
			},
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
	dir := path.Dir(filePath)
	if path.Base(dir) == "templates" {
		return path.Dir(dir)
	}
	return dir
}

// helmDefinitionChartDir returns the chart root directory for a values.yaml
// path: the directory the values file lives in
// (`<chart>/values.yaml` -> `<chart>`).
func helmDefinitionChartDir(filePath string) string {
	return path.Dir(filePath)
}
