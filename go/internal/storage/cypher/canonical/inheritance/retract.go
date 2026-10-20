// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import "fmt"

// InheritanceRetractChildLabels lists the child node labels an inheritance edge
// (INHERITS/OVERRIDES/ALIASES/IMPLEMENTS) can originate from.
var InheritanceRetractChildLabels = []string{"Function", "Class", "Interface", "Trait", "Struct", "Enum", "Protocol"}

// InheritanceRetractParts emits one retract part per child label.
//
// A single statement cannot bind all child labels on NornicDB: a node-label
// disjunction MATCH (child:Function|Class|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (child) scan is unreliable — it silently
// drops some child labels. Single-label MATCH is the only shape that reliably
// matches every child on both pinned versions, so the retract fans out to one
// part per label (#5116/#4367). The relationship-type disjunction and the
// scope predicate are unchanged per part. scopeField is the child property
// the retract binds ("repo_id" or "path"); scopeParam is the Cypher parameter
// key carrying scopeValues. The parts run sequentially, not grouped (see
// executeInheritanceRetractStatements). The root buildInheritanceRetractStatements
// wrapper maps these parts onto Statements.
func InheritanceRetractParts(scopeField, scopeParam string, scopeValues []string, evidenceSource string) []Part {
	stmts := make([]Part, 0, len(InheritanceRetractChildLabels))
	for _, label := range InheritanceRetractChildLabels {
		cypher := fmt.Sprintf(
			"MATCH (child:%s)-[rel:INHERITS|OVERRIDES|ALIASES|IMPLEMENTS]->()\nWHERE child.%s IN $%s\n  AND rel.evidence_source = $evidence_source\nDELETE rel",
			label, scopeField, scopeParam,
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
