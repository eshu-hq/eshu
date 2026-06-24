// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

const retractInheritanceEdgesByFileCypher = `MATCH (child)-[rel:INHERITS|OVERRIDES|ALIASES|IMPLEMENTS]->()
WHERE child.path IN $file_paths
  AND rel.evidence_source = $evidence_source
DELETE rel`

// BuildRetractInheritanceEdgesByFilePath builds an inheritance edge retraction
// statement for child entities owned by the given repo-qualified file paths.
func BuildRetractInheritanceEdgesByFilePath(filePaths []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractInheritanceEdgesByFileCypher,
		Parameters: map[string]any{
			"file_paths":      filePaths,
			"evidence_source": evidenceSource,
		},
	}
}
