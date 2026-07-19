// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
)

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
MATCH (target:ShellCommand {repo_id: repo_id})
MATCH ()-[rel:EXECUTES_SHELL]->(target)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractShellExecEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (target:ShellCommand {path: file_path})
MATCH ()-[rel:EXECUTES_SHELL]->(target)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// This file's orphan ShellCommand cleanup is a Go-side anti-join, not a
// Cypher relationship-existence predicate: on the pinned NornicDB backends
// `NOT (target)--()`, `(target)--()`, and `COUNT { (target)--() } = 0` are all
// mis-evaluated (see docs/public/reference/nornicdb-pitfalls.md, "Every
// Relationship-Existence Predicate Is Mis-Evaluated"). The predecessor
// `COUNT { (target)--() } = 0` shape was proven only to fire a DELETE on a
// node already known to be an orphan; it was never proven to preserve a
// connected node, and #5147 later showed the same predicate class is a
// permanently-true tautology that would delete every in-scope ShellCommand,
// connected or not. The shape here mirrors orphan_sweep.go: S1 reads
// candidate uids, S2 reads which of those uids currently have any
// relationship via a concrete relationship variable, the anti-join
// (candidates minus connected) is computed in Go, and only the resulting
// non-connected uids are deleted by explicit key.

// shellCommandCandidateKeysByRepoCypher is the S1 read for a whole-repo
// shell-exec retract: every in-scope ShellCommand uid for the given
// repositories and evidence source. It carries no relationship predicate.
const shellCommandCandidateKeysByRepoCypher = `UNWIND $repo_ids AS repo_id
MATCH (target:ShellCommand {repo_id: repo_id})
WHERE target.evidence_source = $evidence_source
RETURN DISTINCT target.uid AS key`

// shellCommandCandidateKeysByFileCypher is the S1 read for a delta shell-exec
// retract: every in-scope ShellCommand uid for the given source file paths
// and evidence source. It carries no relationship predicate.
const shellCommandCandidateKeysByFileCypher = `UNWIND $file_paths AS file_path
MATCH (target:ShellCommand {path: file_path})
WHERE target.evidence_source = $evidence_source
RETURN DISTINCT target.uid AS key`

// shellCommandConnectedKeysCypher is the S2 read: for each candidate uid,
// whether that ShellCommand currently has any relationship. This is the only
// relationship-existence primitive proven reliable on both pinned NornicDB
// backends -- a concrete relationship variable in a MATCH anchored on a
// caller-supplied key. The UNWIND binding variable is deliberately named
// candidate_key rather than key: reusing the RETURN alias name for the UNWIND
// variable silently returns zero rows on the pinned NornicDB backends instead
// of erroring (see docs/public/reference/nornicdb-pitfalls.md).
const shellCommandConnectedKeysCypher = `UNWIND $keys AS candidate_key
MATCH (target:ShellCommand {uid: candidate_key})-[r]-(m)
RETURN DISTINCT target.uid AS key`

// deleteShellCommandsByUIDCypher is the S3 write: deletes exactly the
// supplied uids, computed by the caller as candidates minus connected. It is
// a key-anchored DELETE, never DETACH DELETE and never a relationship
// predicate.
const deleteShellCommandsByUIDCypher = `UNWIND $keys AS candidate_key
MATCH (target:ShellCommand {uid: candidate_key})
DELETE target`

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

// retractShellExecEdges deletes shell-exec edges for repoIDs, then runs the
// anti-join orphan ShellCommand cleanup, each as its own transaction --
// deliberately sequential, not grouped through ExecuteGroup. On NornicDB
// v1.1.11 multiple DELETE statements sharing one managed transaction do not
// all apply (measured: the grouped path left the in-scope EXECUTES_SHELL edge
// undeleted); running the edge delete first as its own auto-commit
// transaction, then the cleanup's own reads and write, keeps every step
// independently scoped and idempotent.
func (w *EdgeWriter) retractShellExecEdges(ctx context.Context, repoIDs []string, evidenceSource string) error {
	if err := w.executor.Execute(ctx, BuildRetractShellExecEdges(repoIDs, evidenceSource)); err != nil {
		return WrapRetryableNeo4jError(err)
	}
	return w.cleanupOrphanShellCommands(ctx, shellCommandCandidateKeysByRepoCypher, map[string]any{
		"repo_ids":        repoIDs,
		"evidence_source": evidenceSource,
	})
}

// retractShellExecEdgesByFilePath deletes shell-exec edges for filePaths,
// then runs the anti-join orphan ShellCommand cleanup, each as its own
// transaction, for the same NornicDB v1.1.11 managed-transaction reason
// documented on retractShellExecEdges.
func (w *EdgeWriter) retractShellExecEdgesByFilePath(ctx context.Context, filePaths []string, evidenceSource string) error {
	if err := w.executor.Execute(ctx, BuildRetractShellExecEdgesByFilePath(filePaths, evidenceSource)); err != nil {
		return WrapRetryableNeo4jError(err)
	}
	return w.cleanupOrphanShellCommands(ctx, shellCommandCandidateKeysByFileCypher, map[string]any{
		"file_paths":      filePaths,
		"evidence_source": evidenceSource,
	})
}

// cleanupOrphanShellCommands runs the S1/S2 anti-join reads for the given
// candidate-keys query and deletes exactly the resulting non-connected uids.
// It issues zero reads and zero writes when the S1 read finds no candidates,
// and issues no write when every candidate is still connected.
func (w *EdgeWriter) cleanupOrphanShellCommands(
	ctx context.Context,
	candidateCypher string,
	candidateParams map[string]any,
) error {
	if w.Reader == nil {
		return fmt.Errorf("edge writer reader is required for shell command orphan cleanup")
	}

	candidateKeys, err := w.readShellCommandKeys(ctx, candidateCypher, candidateParams)
	if err != nil {
		return fmt.Errorf("read shell command candidate keys: %w", err)
	}
	if len(candidateKeys) == 0 {
		return nil
	}

	connectedKeys, err := w.readShellCommandKeys(ctx, shellCommandConnectedKeysCypher, map[string]any{
		"keys": candidateKeys,
	})
	if err != nil {
		return fmt.Errorf("read shell command connected keys: %w", err)
	}
	connected := make(map[string]bool, len(connectedKeys))
	for _, key := range connectedKeys {
		connected[key] = true
	}

	orphanKeys := make([]string, 0, len(candidateKeys))
	for _, key := range candidateKeys {
		if !connected[key] {
			orphanKeys = append(orphanKeys, key)
		}
	}
	if len(orphanKeys) == 0 {
		return nil
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    deleteShellCommandsByUIDCypher,
		Parameters: map[string]any{
			"keys": orphanKeys,
		},
	}
	if err := w.executor.Execute(ctx, stmt); err != nil {
		return WrapRetryableNeo4jError(err)
	}
	return nil
}

// readShellCommandKeys runs cypher/params through w.Reader and decodes the
// "key" column of every row into a sorted, deduplicated slice.
func (w *EdgeWriter) readShellCommandKeys(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]string, error) {
	rows, err := w.Reader.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rows))
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		key, ok := row["key"].(string)
		if !ok || key == "" {
			return nil, fmt.Errorf("unexpected shell command key type %T", row["key"])
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
