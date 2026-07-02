// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

const batchCanonicalShellExecUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function {uid: row.source_entity_id})
MERGE (target:ShellCommand {uid: row.target_entity_id})
ON CREATE SET target.evidence_source = row.evidence_source
SET target.type = 'shell_command',
    target.name = row.name,
    target.repo_id = row.repo_id,
    target.path = row.source_path,
    target.line_number = row.line_number,
    target.api = row.api,
    target.language = row.language
MERGE (source)-[rel:EXECUTES_SHELL]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser command-call evidence resolved a shell execution edge',
    rel.evidence_source = row.evidence_source`

const retractShellExecEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:Function {repo_id: repo_id})
MATCH (source)-[rel:EXECUTES_SHELL]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractShellExecEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:Function {path: file_path})
MATCH (source)-[rel:EXECUTES_SHELL]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

func buildShellExecRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	sourceEntityID := payloadString(payload, "source_entity_id")
	targetEntityID := payloadString(payload, "target_entity_id")
	repoID := payloadString(payload, "repo_id")
	sourcePath := payloadString(payload, "source_path")
	if sourceEntityID == "" || targetEntityID == "" || repoID == "" || sourcePath == "" {
		return "", nil, false
	}
	return batchCanonicalShellExecUpsertCypher, map[string]any{
		"source_entity_id":  sourceEntityID,
		"target_entity_id":  targetEntityID,
		"repo_id":           repoID,
		"source_path":       sourcePath,
		"name":              "command execution",
		"line_number":       payloadInt(payload, "line_number"),
		"api":               payloadString(payload, "api"),
		"language":          payloadString(payload, "language"),
		"relationship_type": payloadString(payload, "relationship_type"),
		"evidence_source":   evidenceSource,
	}, true
}

// BuildRetractShellExecEdges builds shell execution edge retraction for repos.
func BuildRetractShellExecEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractShellExecEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractShellExecEdgesByFilePath builds shell execution edge retraction
// for repo-qualified source file paths.
func BuildRetractShellExecEdgesByFilePath(filePaths []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractShellExecEdgesByFileCypher,
		Parameters: map[string]any{
			"file_paths":      filePaths,
			"evidence_source": evidenceSource,
		},
	}
}
