// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// canonicalPhaseGCPCloudResourceEdge names the GCP relationship edge projection
// phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseGCPCloudResourceEdge = "gcp_cloud_resource_edge"

// canonicalGCPCloudResourceEdgeUpsertCypherFormat batches GCP relationship edge
// upserts between two already-materialized CloudResource nodes. The Cypher
// relationship type is a sanitized static token derived from the observed Cloud
// Asset Inventory relationship type and prefixed `GCP_`, so GCP relationship
// edges stay provider-distinct from AWS edges while sharing the CloudResource
// node substrate. Keeping that token out of the relationship MERGE property map
// is required for NornicDB to use its relationship hot path while preserving one
// edge per (source uid, relationship type, target uid). Two MATCHes precede the
// MERGE so a row whose source or target node is absent produces no edge and no
// fabricated node.
const canonicalGCPCloudResourceEdgeUpsertCypherFormat = `UNWIND $rows AS row
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

// retractGCPCloudResourceEdgesCypher removes the reducer-owned GCP relationship
// edges for a set of scopes before a fresh generation reprojects them. GCP
// relationship writes use relationship-type-specific Cypher relationship types,
// so the retract matches any CloudResource relationship and then scopes by the
// edge properties owned by this reducer (evidence_source and the edge's own
// scope_id, not the endpoint node's).
const retractGCPCloudResourceEdgesCypher = `MATCH (source:CloudResource)-[rel]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// GCPCloudResourceEdgeWriter materializes resolved gcp_cloud_relationship facts
// into canonical GCP relationship edges between CloudResource nodes. It mirrors
// the AWS CloudResourceEdgeWriter, satisfies the reducer-owned edge-writer
// consumer interface, and writes through the backend-neutral Executor seam.
type GCPCloudResourceEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewGCPCloudResourceEdgeWriter returns a GCPCloudResourceEdgeWriter backed by
// the given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewGCPCloudResourceEdgeWriter(executor Executor, batchSize int) *GCPCloudResourceEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &GCPCloudResourceEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceEdges upserts GCP relationship edges for the given resolved
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
func (w *GCPCloudResourceEdgeWriter) WriteCloudResourceEdges(
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
		return fmt.Errorf("gcp cloud resource edge writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	cypherTypes := make([]string, 0, len(rows))
	for _, row := range rows {
		cypherType, err := canonicalGCPRelationshipCypherType(row)
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
		cypher := fmt.Sprintf(canonicalGCPCloudResourceEdgeUpsertCypherFormat, cypherType)
		batches := buildBatchedStatements(cypher, grouped[cypherType], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseGCPCloudResourceEdge
			batches[index].Parameters[StatementMetadataEntityLabelKey] = "GCP_RELATIONSHIP"
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"edge=GCP_RELATIONSHIP type=%s rows=%d",
				cypherType,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}

	return w.dispatch(ctx, stmts)
}

// canonicalGCPRelationshipCypherType returns the sanitized `GCP_`-prefixed
// Cypher relationship type for one edge row. The provider relationship type is
// validated to the `[A-Za-z0-9_]` charset and preserved verbatim (case
// included) so two CAI types never collapse onto one edge; the raw value is also
// stored as the rel.relationship_type property for readback truth. The reducer's
// extraction step pre-validates and skips unsupported tokens, so this is a
// defense-in-depth guard, not the primary filter.
func canonicalGCPRelationshipCypherType(row map[string]any) (string, error) {
	raw, ok := row["relationship_type"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("gcp relationship_type must be a non-empty string")
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("gcp relationship_type %q contains unsupported character %q", raw, ch)
	}
	return "GCP_" + raw, nil
}

// RetractCloudResourceEdges removes this reducer's GCP relationship edges for
// the given scopes before a fresh generation reprojects them. It is a no-op for
// an empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches CloudResource nodes.
func (w *GCPCloudResourceEdgeWriter) RetractCloudResourceEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("gcp cloud resource edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractGCPCloudResourceEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseGCPCloudResourceEdge,
			StatementMetadataEntityLabelKey: "GCP_RELATIONSHIP",
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=GCP_RELATIONSHIP retract scopes=%d generation=%s",
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *GCPCloudResourceEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
