// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCloudResource names the AWS CloudResource node materialization
// phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseCloudResource = "cloud_resource"

// baseCloudResourceUpsertCypher batches CloudResource node upserts. MERGE
// is on the stable uid identity only; mutable properties are SET separately so
// duplicate input rows and reducer retries converge on one node rather than
// fabricating or duplicating graph state. The shape mirrors the proven
// TerraformResource canonical writer so it engages the same NornicDB
// schema-backed uid lookup and Neo4j planner path.
const baseCloudResourceUpsertCypher = `UNWIND $rows AS row
MERGE (r:CloudResource {uid: row.uid})
SET r.id = row.uid,
    r.arn = row.arn,
    r.resource_id = row.resource_id,
    r.resource_type = row.resource_type,
    r.name = row.name,
    r.state = row.state,
    r.account_id = row.account_id,
    r.region = row.region,
    r.service_kind = row.service_kind,
    r.correlation_anchors = row.correlation_anchors,
    r.service_anchor_status = row.service_anchor_status,
    r.service_anchor_source = row.service_anchor_source,
    r.service_anchor_reason = row.service_anchor_reason,
    r.service_anchor_names = row.service_anchor_names,
    r.service_anchor_name_tokens = row.service_anchor_name_tokens,
    r.workload_id = row.workload_id,
    r.service_name = row.service_name,
    r.running_image_ref = row.running_image_ref,
    r.running_image_digest = row.running_image_digest,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.evidence_source = row.evidence_source`

// canonicalCloudResourceUpsertCypher is the statement WriteCloudResourceNodes
// actually executes: baseCloudResourceUpsertCypher plus
// teethCloudResourceUpsertExtraSet, which is the empty string in every
// normal build (cloud_resource_node_writer_teeth_off.go) and exactly one
// extra SET clause under the ifadeterminismteeth build tag
// (cloud_resource_node_writer_teeth.go) — see that file's doc for why. Both
// operands are untyped string constants, so this concatenation is itself a
// compile-time constant; no normal build pays a runtime cost for the split.
const canonicalCloudResourceUpsertCypher = baseCloudResourceUpsertCypher + teethCloudResourceUpsertExtraSet

// CloudResourceNodeWriter materializes aws_resource facts into canonical
// CloudResource graph nodes. It satisfies the reducer-owned
// CloudResourceNodeWriter consumer interface and writes through the
// backend-neutral Executor seam.
type CloudResourceNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewCloudResourceNodeWriter returns a CloudResourceNodeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewCloudResourceNodeWriter(executor Executor, batchSize int) *CloudResourceNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CloudResourceNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceNodes upserts CloudResource nodes for the given rows using
// batched UNWIND statements. When the executor implements GroupExecutor all
// batches are dispatched in a single atomic transaction; otherwise they run
// sequentially. The write is idempotent: the same uid converges on one node
// across batches, retries, and generations.
func (w *CloudResourceNodeWriter) WriteCloudResourceNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource node writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		annotated = append(annotated, cloned)
	}

	stmts := buildBatchedStatements(canonicalCloudResourceUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCloudResource
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "CloudResource"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=CloudResource rows=%d",
			len(batchRows),
		)
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
