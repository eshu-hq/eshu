// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

import (
	"fmt"
)

// CodeCallRetractSourceLabels lists the source node labels a code-call edge
// (CALLS/REFERENCES/INSTANTIATES) can originate from.
var CodeCallRetractSourceLabels = []string{"Function", "Class", "Struct", "Interface", "TypeAlias", "File"}

// CodeCallMetaclassRetractSourceLabels lists the source labels a USES_METACLASS
// edge can originate from — a narrower set than the code-call edges. Exported
// (like CodeCallRetractSourceLabels) because the root wrapper tests pin the
// per-label fan-out against it.
var CodeCallMetaclassRetractSourceLabels = []string{"Function", "Class", "File"}

// codeCallRetractRelTypes returns the relationship-type disjunction the retract
// deletes for the given evidence source. Relationship-type disjunction is
// supported on NornicDB; node-label disjunction is not (#5116).
func codeCallRetractRelTypes(evidenceSource string) string {
	switch evidenceSource {
	case "parser/code-calls":
		return "CALLS|REFERENCES|INSTANTIATES"
	case "parser/python-metaclass":
		return "USES_METACLASS"
	default:
		return "CALLS|REFERENCES|USES_METACLASS|INSTANTIATES"
	}
}

// CodeCallRetractSourceLabelsFor returns the source labels the retract must
// cover for the given evidence source. Exported alongside the label lists so
// the root single-label-anchor guard test can enumerate the covered labels.
func CodeCallRetractSourceLabelsFor(evidenceSource string) []string {
	if evidenceSource == "parser/python-metaclass" {
		return CodeCallMetaclassRetractSourceLabels
	}
	return CodeCallRetractSourceLabels
}

// buildCodeCallRetractStatements emits one retract statement per source label.
//
// A single statement cannot bind all source labels on NornicDB: a node-label
// disjunction MATCH (source:Function|Class|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (source) scan is unreliable — it silently
// drops some source labels (e.g. File-sourced REFERENCES), inconsistently by
// internal label-iteration state (#5116). Single-label MATCH is the only shape
// that reliably matches every source on both pinned versions, so the retract
// fans out to one statement per label. The relationship-type disjunction and the
// scope predicate are unchanged per statement. scopeField is the source property
// the retract binds ("repo_id" or "path"); scopeParam is the Cypher parameter
// key carrying scopeValues.
// BuildCodeCallRetractParts emits one retract part per source label.
//
// A single statement cannot bind all source labels on NornicDB: a node-label
// disjunction MATCH (source:Function|Class|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (source) scan is unreliable — it silently
// drops some source labels (e.g. File-sourced REFERENCES), inconsistently by
// internal label-iteration state (#5116). Single-label MATCH is the only shape
// that reliably matches every source on both pinned versions, so the retract
// fans out to one part per label. The relationship-type disjunction and the
// scope predicate are unchanged per part. scopeField is the source property
// the retract binds ("repo_id" or "path"); scopeParam is the Cypher parameter
// key carrying scopeValues. The root BuildRetractCodeCallEdgeStatements
// wrappers map these parts onto Statements.
func BuildCodeCallRetractParts(scopeField, scopeParam string, scopeValues []string, evidenceSource string) []Part {
	relTypes := codeCallRetractRelTypes(evidenceSource)
	labels := CodeCallRetractSourceLabelsFor(evidenceSource)
	stmts := make([]Part, 0, len(labels))
	for _, label := range labels {
		cypher := fmt.Sprintf(
			"MATCH (source:%s)-[rel:%s]->()\nWHERE source.%s IN $%s\n  AND rel.evidence_source = $evidence_source\nDELETE rel",
			label, relTypes, scopeField, scopeParam,
		)
		stmts = append(stmts, Part{
			Cypher: cypher,
			Parameters: map[string]any{
				scopeParam:        scopeValues,
				"evidence_source": evidenceSource,
			},
			IsRetract: true,
		})
	}
	return stmts
}
