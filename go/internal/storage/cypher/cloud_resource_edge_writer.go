// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// canonicalPhaseCloudResourceEdge names the AWS relationship edge projection
// phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseCloudResourceEdge = "cloud_resource_edge"

// canonicalCloudResourceEdgeUpsertCypherFormat batches AWS relationship edge
// upserts between two already-materialized CloudResource nodes. The Cypher
// relationship type is a sanitized static token derived from the observed AWS
// relationship_type. Keeping that token out of a relationship MERGE property
// map is required for NornicDB to use its relationship hot path while preserving
// one edge per (source uid, relationship type, target uid). Two MATCHes precede
// the MERGE so a row whose source or target node is absent produces no edge and
// no fabricated node.
const canonicalCloudResourceEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.relationship_type = row.relationship_type,
    rel.target_type = row.target_type,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractCloudResourceEdgesCypher removes the reducer-owned AWS relationship
// edges for a set of scopes before a fresh generation reprojects them. AWS
// relationship writes use relationship-type-specific Cypher relationship types,
// so the retract intentionally matches any CloudResource relationship and then
// scopes by the edge properties owned by this reducer. The scope predicate
// filters on the edge's own scope_id, not the endpoint node's.
const retractCloudResourceEdgesCypher = `MATCH (source:CloudResource)-[rel]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractCloudResourceEdgesByUIDsCypher is the ledger-anchored counterpart of
// retractCloudResourceEdgesCypher: it enumerates source CloudResource uids via
// a `WHERE source.uid IN $source_uids` predicate instead of scanning the whole
// :CloudResource label. `source.uid IN $source_uids` (a single-clause MATCH
// with the IN-list predicate on the source node, not a separate UNWIND +
// property-map MATCH) seeds the CloudResource.uid index so the engine expands
// adjacency for only the ledger-enumerated sources instead of the whole label;
// this depends on the NornicDB IN-list start-node index seed fix
// (orneryd/NornicDB#258) and measured ~300s/timeout on the prior UNWIND-based
// shape. The single-clause form also binds the relationship correctly, unlike
// a two-clause `MATCH (source) MATCH ()-[rel]->()` split (orneryd/NornicDB#257).
// A bare `[rel]` (no relationship-type literal) is intentional and safe here:
// AWS relationship edges use an open, per-relationship-type Cypher token
// (AWS_<relationship_type>) rather than one fixed type, so enumerating types
// would require an ever-growing allowlist; the `evidence_source` predicate
// already scopes the delete to only this writer's edges (no other writer
// stamps rel.evidence_source = "reducer/aws-relationships"), so a bare `[rel]`
// cannot reach an edge owned by a different writer.
const retractCloudResourceEdgesByUIDsCypher = `MATCH (source:CloudResource)-[rel]->()
WHERE source.uid IN $source_uids
  AND rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// cloudResourceEdgeRetractUIDBatchSize bounds the number of source uids
// passed in the $source_uids IN-list per anchored-retract statement,
// mirroring codeInterprocEvidenceRetractUIDBatchSize.
const cloudResourceEdgeRetractUIDBatchSize = 500

// CloudResourceEdgeWriter materializes resolved aws_relationship facts into
// canonical AWS relationship edges between CloudResource nodes. It satisfies
// the reducer-owned edge-writer consumer interface and writes through the
// backend-neutral Executor seam.
type CloudResourceEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewCloudResourceEdgeWriter returns a CloudResourceEdgeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewCloudResourceEdgeWriter(executor Executor, batchSize int) *CloudResourceEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CloudResourceEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceEdges upserts AWS relationship edges for the given resolved
// rows using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (source_uid, relationship_type, target_uid) converges on one edge across
// batches, retries, and generations, and a missing endpoint is a no-op rather
// than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. The resolution layer does not carry these reducer-scoped
// fields, so the writer is the single place that stamps them; rel.scope_id is
// what the prior-generation retract filters on (CloudResource nodes carry no
// scope_id), so omitting it would make scope-scoped retract a silent no-op.
func (w *CloudResourceEdgeWriter) WriteCloudResourceEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource edge writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	cypherTypes := make([]string, 0, len(rows))
	for _, row := range rows {
		cypherType, err := canonicalAWSRelationshipCypherType(row)
		if err != nil {
			return err
		}
		cloned := make(map[string]any, len(row)+3)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["scope_id"] = scopeID
		cloned["generation_id"] = generationID
		cloned["evidence_source"] = evidenceSource
		if _, exists := grouped[cypherType]; !exists {
			cypherTypes = append(cypherTypes, cypherType)
		}
		grouped[cypherType] = append(grouped[cypherType], cloned)
	}
	sort.Strings(cypherTypes)

	var stmts []Statement
	for _, cypherType := range cypherTypes {
		cypher := fmt.Sprintf(canonicalCloudResourceEdgeUpsertCypherFormat, cypherType)
		batches := buildBatchedStatements(cypher, grouped[cypherType], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCloudResourceEdge
			batches[index].Parameters[StatementMetadataEntityLabelKey] = "AWS_RELATIONSHIP"
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"edge=AWS_RELATIONSHIP type=%s rows=%d",
				cypherType,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}

	return w.dispatch(ctx, stmts)
}

func canonicalAWSRelationshipCypherType(row map[string]any) (string, error) {
	raw, ok := row["relationship_type"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("aws relationship_type must be a non-empty string")
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("aws relationship_type %q contains unsupported character %q", raw, ch)
	}
	return "AWS_" + raw, nil
}

// RetractCloudResourceEdges removes this reducer's AWS relationship edges for
// the given scopes before a fresh generation reprojects them. It is a no-op for
// an empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches CloudResource nodes.
func (w *CloudResourceEdgeWriter) RetractCloudResourceEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCloudResourceEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCloudResourceEdge,
			StatementMetadataEntityLabelKey: "AWS_RELATIONSHIP",
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=AWS_RELATIONSHIP retract scopes=%d generation=%s",
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// RetractCloudResourceEdgesByUIDs removes this reducer's AWS relationship
// edges for the given scopes, enumerating source CloudResource uids from the
// projected-source ledger instead of scanning the whole :CloudResource label.
// It is a no-op for an empty uid set. The delete is scoped to the reducer's
// evidence_source and never touches CloudResource nodes.
func (w *CloudResourceEdgeWriter) RetractCloudResourceEdgesByUIDs(
	ctx context.Context,
	sourceUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(sourceUIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource edge writer executor is required")
	}
	batches := chunkStrings(sourceUIDs, cloudResourceEdgeRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches))
	for _, batch := range batches {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractCloudResourceEdgesByUIDsCypher,
			Parameters: map[string]any{
				"source_uids":                   batch,
				"scope_ids":                     scopeIDs,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       canonicalPhaseCloudResourceEdge,
				StatementMetadataEntityLabelKey: "AWS_RELATIONSHIP",
				StatementMetadataSummaryKey: fmt.Sprintf(
					"edge=AWS_RELATIONSHIP retract_by_uids scopes=%d uids=%d",
					len(scopeIDs),
					len(batch),
				),
			},
		})
	}
	return w.dispatchRetract(ctx, stmts)
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *CloudResourceEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// dispatchRetract routes anchored-retract statements through sequential
// Execute calls, never ExecuteGroup. This avoids a NornicDB v1.1.9 bolt driver
// bug where UNWIND … MATCH … -[rel]-> … DELETE rel inside session.ExecuteWrite
// / tx.Run returns zero rows (the MATCH on the relationship finds nothing),
// even though the same statement via session.Run (autocommit) produces
// correct results. See cypher.CodeInterprocEvidenceWriter.dispatchRetract for
// the same rationale applied to the code-interproc anchored retract.
func (w *CloudResourceEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
