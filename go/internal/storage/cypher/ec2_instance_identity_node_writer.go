// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const (
	canonicalPhaseEC2InstanceIdentity = "ec2_instance_identity"
	ec2InstanceIdentityNodeLabel      = "CloudResource:EC2InstanceIdentity"
)

// canonicalEC2InstanceIdentityUpdateCypher anchors with MERGE, not a bare
// MATCH, for the same reason canonicalRDSPostureUpdateCypher does (issue
// #5652: a bare-MATCH-anchored UNWIND SET can silently drop its write on the
// pinned production NornicDB image). MERGE is safe here only because
// WriteEC2InstanceIdentityNodes only ever MERGEs a uid that
// filterRowsToExistingCloudResourceUIDs already confirmed exists via a
// separate read — this writer's never-create contract — so MERGE always
// matches and never creates.
//
// Every SET property here is disjoint from canonicalEC2InstanceUpsertCypher's
// (ec2_instance_node_writer.go) SET list: ami_id and the ec2_identity_*
// provenance fields never collide with the posture writer's base identity or
// posture properties, so the two domains' writes to the SAME CloudResource
// node commute regardless of dispatch order.
const canonicalEC2InstanceIdentityUpdateCypher = `UNWIND $rows AS row
MERGE (r:CloudResource {uid: row.uid})
SET r.ami_id = row.ami_id,
    r.ec2_identity_scope_id = row.scope_id,
    r.ec2_identity_generation_id = row.generation_id,
    r.ec2_identity_evidence_source = row.evidence_source,
    r.ec2_identity_source_fact_id = row.source_fact_id`

const retractEC2InstanceIdentityPropertiesCypher = `MATCH (r:CloudResource)
WHERE r.ec2_identity_scope_id IN $scope_ids
  AND r.ec2_identity_evidence_source = $evidence_source
REMOVE r.ami_id,
       r.ec2_identity_scope_id,
       r.ec2_identity_generation_id,
       r.ec2_identity_evidence_source,
       r.ec2_identity_source_fact_id`

// EC2InstanceIdentityNodeWriter updates the #5448 ami_id property on existing
// EC2 instance CloudResource nodes. It never creates nodes: WriteEC2InstanceIdentityNodes
// reads which candidate uids already exist first and drops rows for uids that
// do not, so a missing uid is a no-op before the write ever runs — the node is
// owned by EC2InstanceNodeWriter (ec2_instance_node_writer.go).
type EC2InstanceIdentityNodeWriter struct {
	executor  Executor
	reader    PostureExistenceReader
	batchSize int
}

// NewEC2InstanceIdentityNodeWriter returns an EC2InstanceIdentityNodeWriter
// backed by the given Executor and PostureExistenceReader. A batchSize of 0 or
// less uses DefaultBatchSize.
func NewEC2InstanceIdentityNodeWriter(executor Executor, reader PostureExistenceReader, batchSize int) *EC2InstanceIdentityNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &EC2InstanceIdentityNodeWriter{executor: executor, reader: reader, batchSize: batchSize}
}

// WriteEC2InstanceIdentityNodes stamps reducer-owned ami_id properties onto
// existing CloudResource nodes using a batched MATCH+SET statement. The write
// is idempotent: retries update the same uid and never fabricate a missing
// node.
func (w *EC2InstanceIdentityNodeWriter) WriteEC2InstanceIdentityNodes(
	ctx context.Context,
	rows []map[string]any,
	scopeID, generationID, evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 instance identity node writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	existing, err := filterRowsToExistingCloudResourceUIDs(ctx, w.reader, annotated)
	if err != nil {
		return fmt.Errorf("ec2 instance identity node writer: %w", err)
	}
	if len(existing) == 0 {
		return nil
	}

	stmts := buildBatchedStatements(canonicalEC2InstanceIdentityUpdateCypher, existing, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseEC2InstanceIdentity
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = ec2InstanceIdentityNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			ec2InstanceIdentityNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractEC2InstanceIdentityNodes removes only reducer-owned EC2 instance
// identity properties for the given scopes. It leaves CloudResource identity,
// posture, and every other domain's properties untouched.
func (w *EC2InstanceIdentityNodeWriter) RetractEC2InstanceIdentityNodes(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 instance identity node writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractEC2InstanceIdentityPropertiesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseEC2InstanceIdentity,
			StatementMetadataEntityLabelKey: ec2InstanceIdentityNodeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				ec2InstanceIdentityNodeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func (w *EC2InstanceIdentityNodeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract runs retract statements sequentially through Execute, each
// in its own auto-commit transaction — never ExecuteGroup. On NornicDB v1.1.11
// a retract inside a managed transaction can under-apply even as a single
// statement (#4367/#5128/#5146/#5152). RetractEC2InstanceIdentityNodes routes
// through this so the retract is never batched with a sibling write via
// ExecuteGroup.
func (w *EC2InstanceIdentityNodeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
