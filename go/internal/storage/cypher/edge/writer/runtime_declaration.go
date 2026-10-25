// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"sort"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	canonicalPhaseRuntimeDeclaration = "runtime_declaration"
	runtimeDeclarationLabel          = "RuntimeDeclaration"
)

// RuntimeDeclarationUpsertCypher upserts one declared runtime version as a
// RuntimeDeclaration node owned by its Repository (issue #8816).
//
// The node MERGE keys on exactly the graph key the #8815 arbiter ruling fixed
// for runtime_declaration.version: (repo_id, relative_path, line_number,
// runtime, order_index). The runtime_declaration_identity composite uniqueness
// constraint backs it, so on Neo4j the MERGE plans MergeUniqueNode. Every other
// property is SET, never part of the identity: a pin file edited in place
// (".nvmrc" from 18 to 20 on line 1) rewrites the same node, and a SET to null
// removes a version part a range or alias no longer carries.
//
// The Repository is MERGEd inline, mirroring the codeowners DECLARES_CODEOWNER
// writer: the owning repository is the generation being projected, so there is
// no cross-acceptance-unit MATCH dependency and no readiness gate. The
// DECLARES_RUNTIME edge has no identity properties because each
// RuntimeDeclaration node has exactly one owning repository.
const RuntimeDeclarationUpsertCypher = `UNWIND $rows AS row
MERGE (repo:Repository {id: row.repo_id})
MERGE (d:RuntimeDeclaration {repo_id: row.repo_id, relative_path: row.relative_path, line_number: row.line_number, runtime: row.runtime, order_index: row.order_index})
SET d.scope_dir = row.scope_dir,
    d.source_kind = row.source_kind,
    d.tool_name = row.tool_name,
    d.raw_spec = row.raw_spec,
    d.spec_kind = row.spec_kind,
    d.version_major = row.version_major,
    d.version_minor = row.version_minor,
    d.version_patch = row.version_patch,
    d.distribution = row.distribution,
    d.range_lower = row.range_lower,
    d.range_upper = row.range_upper,
    d.range_lower_major = row.range_lower_major,
    d.range_upper_major = row.range_upper_major,
    d.range_lower_inclusive = row.range_lower_inclusive,
    d.range_upper_inclusive = row.range_upper_inclusive,
    d.generation_id = row.generation_id,
    d.evidence_source = row.evidence_source
MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// RetractRuntimeDeclarationsCypher removes every RuntimeDeclaration node this
// evidence source owns for a set of repositories (whole-repository retract,
// used on a full generation). It seeks each Repository by its unique id and
// expands DECLARES_RUNTIME, so it never scans the RuntimeDeclaration label;
// DETACH DELETE takes the edge with the node. No other writer MERGEs the
// RuntimeDeclaration label, so deleting the node cannot strand another
// family's edges.
const RetractRuntimeDeclarationsCypher = `MATCH (repo:Repository)-[:DECLARES_RUNTIME]->(d:RuntimeDeclaration)
WHERE repo.id IN $repo_ids
  AND d.evidence_source = $evidence_source
DETACH DELETE d`

// RetractRuntimeDeclarationsByPathCypher removes the RuntimeDeclaration nodes
// declared by the changed or deleted files of a delta generation. Each scope
// row binds one repository to its own relative paths: a pin file path such as
// ".nvmrc" is identical in every repository, so a flattened repo-list x
// path-list filter could not express "this repo's paths only" once a batch
// held more than one repository (the cross-repo over-retraction #5419 P1
// fixed for CODEOWNERS).
const RetractRuntimeDeclarationsByPathCypher = `UNWIND $scopes AS scope
MATCH (repo:Repository {id: scope.repo_id})-[:DECLARES_RUNTIME]->(d:RuntimeDeclaration)
WHERE d.relative_path IN scope.relative_paths
  AND d.evidence_source = $evidence_source
DETACH DELETE d`

// RuntimeDeclarationWriter materializes reducer-owned RuntimeDeclaration nodes
// and their Repository-[:DECLARES_RUNTIME]-> edges, and retracts them per
// repository or per (repository, file).
type RuntimeDeclarationWriter struct {
	executor  sourcecypher.Executor
	batchSize int
}

// NewRuntimeDeclarationWriter returns a RuntimeDeclarationWriter backed by the
// given Executor. A batchSize of 0 or less uses sourcecypher.DefaultBatchSize.
func NewRuntimeDeclarationWriter(executor sourcecypher.Executor, batchSize int) *RuntimeDeclarationWriter {
	if batchSize <= 0 {
		batchSize = sourcecypher.DefaultBatchSize
	}
	return &RuntimeDeclarationWriter{executor: executor, batchSize: batchSize}
}

// WriteRuntimeDeclarations upserts the given declaration rows in UNWIND
// batches, stamping each row (on a copy) with the generation and evidence
// source. Each row must carry the five identity keys; the reducer handler
// drops rows missing any of them before calling. The upsert is idempotent:
// replaying the same rows writes the same nodes and edges.
func (w *RuntimeDeclarationWriter) WriteRuntimeDeclarations(
	ctx context.Context,
	rows []map[string]any,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("runtime declaration writer executor is required")
	}
	stamped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		stamped = append(stamped, sourcecypher.CloneRowWith(row, map[string]any{
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}
	batches := sourcecypher.BuildBatchedStatements(RuntimeDeclarationUpsertCypher, stamped, w.batchSize)
	for index := range batches {
		batchRows := batches[index].Parameters["rows"].([]map[string]any)
		batches[index].Parameters[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseRuntimeDeclaration
		batches[index].Parameters[sourcecypher.StatementMetadataEntityLabelKey] = runtimeDeclarationLabel
		batches[index].Parameters[sourcecypher.StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d", runtimeDeclarationLabel, len(batchRows),
		)
	}
	return w.execute(ctx, batches)
}

// RetractRuntimeDeclarations removes every RuntimeDeclaration this evidence
// source owns for the given repositories. An empty repoIDs is a no-op.
func (w *RuntimeDeclarationWriter) RetractRuntimeDeclarations(
	ctx context.Context,
	repoIDs []string,
	evidenceSource string,
) error {
	if len(repoIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("runtime declaration writer executor is required")
	}
	return w.execute(ctx, []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    RetractRuntimeDeclarationsCypher,
		Parameters: map[string]any{
			"repo_ids":                                   repoIDs,
			"evidence_source":                            evidenceSource,
			sourcecypher.StatementMetadataPhaseKey:       canonicalPhaseRuntimeDeclaration,
			sourcecypher.StatementMetadataEntityLabelKey: runtimeDeclarationLabel,
			sourcecypher.StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract repos=%d", runtimeDeclarationLabel, len(repoIDs),
			),
		},
	}})
}

// RetractRuntimeDeclarationsByPath removes the RuntimeDeclaration nodes whose
// relative_path is one of the paths listed for their own repository in
// pathsByRepoID. Repositories with no paths are skipped; the scope rows are
// sorted by repository id so the statement is deterministic.
func (w *RuntimeDeclarationWriter) RetractRuntimeDeclarationsByPath(
	ctx context.Context,
	pathsByRepoID map[string][]string,
	evidenceSource string,
) error {
	scopes := runtimeDeclarationPathScopes(pathsByRepoID)
	if len(scopes) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("runtime declaration writer executor is required")
	}
	return w.execute(ctx, []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    RetractRuntimeDeclarationsByPathCypher,
		Parameters: map[string]any{
			"scopes":                               scopes,
			"evidence_source":                      evidenceSource,
			sourcecypher.StatementMetadataPhaseKey: canonicalPhaseRuntimeDeclaration,
			sourcecypher.StatementMetadataEntityLabelKey: runtimeDeclarationLabel,
			sourcecypher.StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract_by_path repos=%d", runtimeDeclarationLabel, len(scopes),
			),
		},
	}})
}

func runtimeDeclarationPathScopes(pathsByRepoID map[string][]string) []map[string]any {
	repoIDs := make([]string, 0, len(pathsByRepoID))
	for repoID, paths := range pathsByRepoID {
		if repoID == "" || len(paths) == 0 {
			continue
		}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	scopes := make([]map[string]any, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		paths := append([]string(nil), pathsByRepoID[repoID]...)
		sort.Strings(paths)
		scopes = append(scopes, map[string]any{"repo_id": repoID, "relative_paths": paths})
	}
	return scopes
}

// execute runs statements one at a time through Execute. Retracts use
// UNWIND/MATCH/DETACH DELETE, which must not ride ExecuteGroup (see
// CodeTaintEvidenceWriter.dispatchRetract); the upsert batches are independent
// per-repository rows, so sequential auto-commit costs no correctness.
func (w *RuntimeDeclarationWriter) execute(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
