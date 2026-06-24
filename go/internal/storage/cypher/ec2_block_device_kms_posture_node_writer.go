// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const canonicalPhaseEC2BlockDeviceKMSPosture = "ec2_block_device_kms_posture"

const ec2BlockDeviceKMSPostureNodeLabel = "CloudResource:EC2BlockDeviceKMSPosture"

const canonicalEC2BlockDeviceKMSPostureNodeUpsertCypher = `UNWIND $rows AS row
MATCH (resource:CloudResource {uid: row.uid})
SET resource.ec2_block_device_kms_state = row.state,
    resource.ec2_block_device_kms_reason = row.reason,
    resource.ec2_block_device_volume_count = row.volume_count,
    resource.ec2_block_device_encrypted_volume_count = row.encrypted_volume_count,
    resource.ec2_block_device_unencrypted_volume_count = row.unencrypted_volume_count,
    resource.ec2_block_device_unresolved_volume_count = row.unresolved_volume_count,
    resource.ec2_block_device_kms_key_count = row.kms_key_count,
    resource.ec2_block_device_volume_ids = row.volume_ids,
    resource.ec2_block_device_kms_key_ids = row.kms_key_ids,
    resource.ec2_block_device_kms_scope_id = row.scope_id,
    resource.ec2_block_device_kms_generation_id = row.generation_id,
    resource.ec2_block_device_kms_evidence_source = row.evidence_source,
    resource.ec2_block_device_kms_source_fact_id = row.source_fact_id`

const retractEC2BlockDeviceKMSPostureNodesCypher = `MATCH (resource:CloudResource)
WHERE resource.ec2_block_device_kms_scope_id IN $scope_ids
  AND resource.ec2_block_device_kms_evidence_source = $evidence_source
REMOVE resource.ec2_block_device_kms_state,
       resource.ec2_block_device_kms_reason,
       resource.ec2_block_device_volume_count,
       resource.ec2_block_device_encrypted_volume_count,
       resource.ec2_block_device_unencrypted_volume_count,
       resource.ec2_block_device_unresolved_volume_count,
       resource.ec2_block_device_kms_key_count,
       resource.ec2_block_device_volume_ids,
       resource.ec2_block_device_kms_key_ids,
       resource.ec2_block_device_kms_scope_id,
       resource.ec2_block_device_kms_generation_id,
       resource.ec2_block_device_kms_evidence_source,
       resource.ec2_block_device_kms_source_fact_id`

// EC2BlockDeviceKMSPostureNodeWriter writes reducer-owned block-device KMS
// posture properties onto already-materialized EC2 CloudResource nodes. It never
// creates CloudResource nodes; a missing uid is a no-op at the MATCH.
type EC2BlockDeviceKMSPostureNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewEC2BlockDeviceKMSPostureNodeWriter returns an
// EC2BlockDeviceKMSPostureNodeWriter backed by the given Executor. A batchSize
// of 0 or less uses DefaultBatchSize (500).
func NewEC2BlockDeviceKMSPostureNodeWriter(executor Executor, batchSize int) *EC2BlockDeviceKMSPostureNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &EC2BlockDeviceKMSPostureNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteEC2BlockDeviceKMSPostureNodes sets reducer-owned posture properties on
// existing EC2 CloudResource nodes. The writer injects scope/generation/evidence
// metadata into each row so retractions can remove only properties owned by this
// reducer.
func (w *EC2BlockDeviceKMSPostureNodeWriter) WriteEC2BlockDeviceKMSPostureNodes(
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
		return fmt.Errorf("ec2 block-device KMS posture node writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	stmts := buildBatchedStatements(canonicalEC2BlockDeviceKMSPostureNodeUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseEC2BlockDeviceKMSPosture
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = ec2BlockDeviceKMSPostureNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"node_properties=%s rows=%d",
			ec2BlockDeviceKMSPostureNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractEC2BlockDeviceKMSPostureNodes removes reducer-owned EC2 block-device
// KMS posture properties for the supplied scopes. It leaves CloudResource nodes
// and unrelated properties in place.
func (w *EC2BlockDeviceKMSPostureNodeWriter) RetractEC2BlockDeviceKMSPostureNodes(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 block-device KMS posture node writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractEC2BlockDeviceKMSPostureNodesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseEC2BlockDeviceKMSPosture,
			StatementMetadataEntityLabelKey: ec2BlockDeviceKMSPostureNodeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"node_properties=%s retract scopes=%d generation=%s",
				ec2BlockDeviceKMSPostureNodeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

func (w *EC2BlockDeviceKMSPostureNodeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
