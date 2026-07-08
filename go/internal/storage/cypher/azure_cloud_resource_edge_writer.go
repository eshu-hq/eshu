// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const canonicalPhaseAzureCloudResourceEdge = "azure_cloud_resource_edge"

const canonicalAzureCloudResourceEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.relationship_type = row.relationship_type,
    rel.target_type = row.target_type,
    rel.support_state = row.support_state,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const canonicalAzureRelationshipTypeManagedBy = "managed_by"

const retractAzureCloudResourceEdgesCypher = `MATCH (source:CloudResource)-[rel]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractAzureCloudResourceEdgesByUIDsCypher is the ledger-anchored
// counterpart of retractAzureCloudResourceEdgesCypher: it enumerates source
// CloudResource uids via a `WHERE source.uid IN $source_uids` predicate
// instead of scanning the whole :CloudResource label. `source.uid IN
// $source_uids` (a single-clause MATCH with the IN-list predicate on the
// source node, not a separate UNWIND + property-map MATCH) seeds the
// CloudResource.uid index so the engine expands adjacency for only the
// ledger-enumerated sources instead of the whole label; this depends on the
// NornicDB IN-list start-node index seed fix (orneryd/NornicDB#258) and
// measured ~300s/timeout on the prior UNWIND-based shape. The single-clause
// form also binds the relationship correctly, unlike a two-clause
// `MATCH (source) MATCH ()-[rel]->()` split (orneryd/NornicDB#257). A bare
// `[rel]` is safe here because the `evidence_source` predicate already scopes
// the delete to only this writer's edges.
const retractAzureCloudResourceEdgesByUIDsCypher = `MATCH (source:CloudResource)-[rel]->()
WHERE source.uid IN $source_uids
  AND rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// azureCloudResourceEdgeRetractUIDBatchSize bounds the number of source uids
// passed in the $source_uids IN-list per anchored-retract statement.
const azureCloudResourceEdgeRetractUIDBatchSize = 500

// AzureCloudResourceEdgeWriter materializes resolved azure_cloud_relationship
// facts into canonical Azure relationship edges between CloudResource nodes.
type AzureCloudResourceEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewAzureCloudResourceEdgeWriter returns an AzureCloudResourceEdgeWriter backed
// by the given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewAzureCloudResourceEdgeWriter(executor Executor, batchSize int) *AzureCloudResourceEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &AzureCloudResourceEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceEdges upserts Azure relationship edges with MATCH-only
// endpoint resolution. Missing CloudResource endpoints produce no edge and no
// fabricated node.
func (w *AzureCloudResourceEdgeWriter) WriteCloudResourceEdges(
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
		return fmt.Errorf("azure cloud resource edge writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	cypherTypes := make([]string, 0, len(rows))
	for _, row := range rows {
		cypherType, err := canonicalAzureRelationshipCypherType(row)
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
		cypher := fmt.Sprintf(canonicalAzureCloudResourceEdgeUpsertCypherFormat, cypherType)
		batches := buildBatchedStatements(cypher, grouped[cypherType], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseAzureCloudResourceEdge
			batches[index].Parameters[StatementMetadataEntityLabelKey] = "AZURE_RELATIONSHIP"
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"edge=AZURE_RELATIONSHIP type=%s rows=%d",
				cypherType,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}
	return w.dispatch(ctx, stmts)
}

func canonicalAzureRelationshipCypherType(row map[string]any) (string, error) {
	raw, ok := row["relationship_type"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("azure relationship_type must be a non-empty string")
	}
	if raw != canonicalAzureRelationshipTypeManagedBy {
		return "", fmt.Errorf("azure relationship_type %q is not supported", raw)
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("azure relationship_type %q contains unsupported character %q", raw, ch)
	}
	return "AZURE_" + raw, nil
}

// RetractCloudResourceEdges removes Azure relationship edges owned by this
// reducer for the given scopes. It deletes relationships only, never endpoint
// CloudResource nodes.
func (w *AzureCloudResourceEdgeWriter) RetractCloudResourceEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("azure cloud resource edge writer executor is required")
	}
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractAzureCloudResourceEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseAzureCloudResourceEdge,
			StatementMetadataEntityLabelKey: "AZURE_RELATIONSHIP",
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=AZURE_RELATIONSHIP retract scopes=%d generation=%s",
				len(scopeIDs),
				generationID,
			),
		},
	}
	return w.dispatch(ctx, []Statement{stmt})
}

// RetractCloudResourceEdgesByUIDs removes Azure relationship edges owned by
// this reducer for the given scopes, enumerating source CloudResource uids
// from the projected-source ledger instead of scanning the whole
// :CloudResource label. It is a no-op for an empty uid set.
func (w *AzureCloudResourceEdgeWriter) RetractCloudResourceEdgesByUIDs(
	ctx context.Context,
	sourceUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(sourceUIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("azure cloud resource edge writer executor is required")
	}
	batches := chunkStrings(sourceUIDs, azureCloudResourceEdgeRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches))
	for _, batch := range batches {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractAzureCloudResourceEdgesByUIDsCypher,
			Parameters: map[string]any{
				"source_uids":                   batch,
				"scope_ids":                     scopeIDs,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       canonicalPhaseAzureCloudResourceEdge,
				StatementMetadataEntityLabelKey: "AZURE_RELATIONSHIP",
				StatementMetadataSummaryKey: fmt.Sprintf(
					"edge=AZURE_RELATIONSHIP retract_by_uids scopes=%d uids=%d",
					len(scopeIDs),
					len(batch),
				),
			},
		})
	}
	return w.dispatchRetract(ctx, stmts)
}

func (w *AzureCloudResourceEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
// Execute calls, never ExecuteGroup, for the same NornicDB bolt-driver reason
// documented on CloudResourceEdgeWriter.dispatchRetract.
func (w *AzureCloudResourceEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
