// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// canonicalPhaseEC2Instance names the EC2 instance CloudResource node
// materialization phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseEC2Instance = "ec2_instance"

// canonicalEC2InstanceUpsertCypher batches EC2 instance CloudResource node
// upserts. MERGE is on the stable uid identity only; mutable properties are SET
// separately so duplicate input rows and reducer retries converge on one node
// rather than fabricating or duplicating graph state. The shape mirrors the
// proven CloudResource canonical writer (the node IS a CloudResource keyed by the
// same cloud_resource_uid identity) so it engages the same NornicDB schema-backed
// uid lookup, the same cloud_resource_uid_unique constraint, and the same Neo4j
// planner path. The only difference from canonicalCloudResourceUpsertCypher is
// the ten derived posture properties; they are SET, never part of the identity.
//
// The node carries metadata-only safe identifiers plus derived posture
// booleans/scalars. It NEVER carries user-data content (only the user_data_present
// boolean), the raw public IP, per-volume block-device maps, or any other
// instance payload. instance_profile_arn rides as a property here; the
// USES_PROFILE edge that consumes it is a later gated slice (#1146 PR-B).
const canonicalEC2InstanceUpsertCypher = `UNWIND $rows AS row
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
    r.imds_v2_required = row.imds_v2_required,
    r.imds_http_endpoint = row.imds_http_endpoint,
    r.imds_http_put_hop_limit = row.imds_http_put_hop_limit,
    r.user_data_present = row.user_data_present,
    r.detailed_monitoring_enabled = row.detailed_monitoring_enabled,
    r.ebs_optimized = row.ebs_optimized,
    r.public_ip_associated = row.public_ip_associated,
    r.instance_profile_arn = row.instance_profile_arn,
    r.tenancy = row.tenancy,
    r.nitro_enclave_enabled = row.nitro_enclave_enabled,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.evidence_source = row.evidence_source`

// EC2InstanceNodeWriter materializes ec2_instance_posture facts into canonical
// :CloudResource graph nodes on the existing cloud_resource_uid keyspace. It
// satisfies the reducer-owned EC2InstanceNodeWriter consumer interface and writes
// through the backend-neutral Executor seam. EC2 instances are deliberately NOT
// emitted as aws_resource inventory facts by the scanner, so this writer is the
// only path that materializes an instance as a node; it never collides with the
// #805 aws_resource node writer on the same uid for the same instance.
type EC2InstanceNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewEC2InstanceNodeWriter returns an EC2InstanceNodeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewEC2InstanceNodeWriter(executor Executor, batchSize int) *EC2InstanceNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &EC2InstanceNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteEC2InstanceNodes upserts EC2 instance CloudResource nodes for the given
// rows using batched UNWIND statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same uid
// converges on one node across batches, retries, and generations.
func (w *EC2InstanceNodeWriter) WriteEC2InstanceNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 instance node writer executor is required")
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

	stmts := buildBatchedStatements(canonicalEC2InstanceUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseEC2Instance
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "CloudResource"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=CloudResource resource_type=%s rows=%d",
			awsv1.ResourceTypeEC2Instance,
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
