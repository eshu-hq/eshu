// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const (
	canonicalPhaseRDSPosture = "rds_posture"
	rdsPostureNodeLabel      = "CloudResource:RDSPosture"
)

// canonicalRDSPostureUpdateCypher anchors with MERGE, not a bare MATCH.
// Issue #5652: on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11) a bare-MATCH-anchored UNWIND SET silently drops
// its write (statement reports success; the property is never persisted).
// MERGE is not a blind substitute for this writer's never-create contract —
// WriteRDSPostureNodes only ever MERGEs a uid that
// filterRowsToExistingCloudResourceUIDs already confirmed exists via a
// separate read, so MERGE always matches and never creates. See
// posture_node_existence.go and
// docs/internal/evidence/5652-nornic-bare-match-writeloss.md.
const canonicalRDSPostureUpdateCypher = `UNWIND $rows AS row
MERGE (r:CloudResource {uid: row.uid})
SET r.rds_identifier = row.rds_identifier,
    r.rds_resource_type = row.rds_resource_type,
    r.rds_engine = row.rds_engine,
    r.rds_publicly_accessible = row.rds_publicly_accessible,
    r.rds_public_exposure_state = row.rds_public_exposure_state,
    r.rds_storage_encrypted = row.rds_storage_encrypted,
    r.rds_kms_key_id = row.rds_kms_key_id,
    r.rds_iam_database_authentication_enabled = row.rds_iam_database_authentication_enabled,
    r.rds_multi_az = row.rds_multi_az,
    r.rds_deletion_protection = row.rds_deletion_protection,
    r.rds_backup_retention_period = row.rds_backup_retention_period,
    r.rds_performance_insights_enabled = row.rds_performance_insights_enabled,
    r.rds_performance_insights_retention_days = row.rds_performance_insights_retention_days,
    r.rds_performance_insights_kms_key_id = row.rds_performance_insights_kms_key_id,
    r.rds_ca_certificate_identifier = row.rds_ca_certificate_identifier,
    r.rds_parameter_groups = row.rds_parameter_groups,
    r.rds_option_groups = row.rds_option_groups,
    r.rds_security_parameters = row.rds_security_parameters,
    r.rds_posture_scope_id = row.scope_id,
    r.rds_posture_generation_id = row.generation_id,
    r.rds_posture_evidence_source = row.evidence_source,
    r.rds_posture_source_fact_id = row.source_fact_id`

const retractRDSPosturePropertiesCypher = `MATCH (r:CloudResource)
WHERE r.rds_posture_scope_id IN $scope_ids
  AND r.rds_posture_evidence_source = $evidence_source
REMOVE r.rds_identifier,
       r.rds_resource_type,
       r.rds_engine,
       r.rds_publicly_accessible,
       r.rds_public_exposure_state,
       r.rds_storage_encrypted,
       r.rds_kms_key_id,
       r.rds_iam_database_authentication_enabled,
       r.rds_multi_az,
       r.rds_deletion_protection,
       r.rds_backup_retention_period,
       r.rds_performance_insights_enabled,
       r.rds_performance_insights_retention_days,
       r.rds_performance_insights_kms_key_id,
       r.rds_ca_certificate_identifier,
       r.rds_parameter_groups,
       r.rds_option_groups,
       r.rds_security_parameters,
       r.rds_posture_scope_id,
       r.rds_posture_generation_id,
       r.rds_posture_evidence_source,
       r.rds_posture_source_fact_id`

// RDSPostureNodeWriter updates RDS posture properties on existing CloudResource
// nodes. It never creates nodes: WriteRDSPostureNodes reads which candidate
// uids already exist first and drops rows for uids that do not, so a missing
// uid is a no-op before the write ever runs.
type RDSPostureNodeWriter struct {
	executor  Executor
	reader    PostureExistenceReader
	batchSize int
}

// NewRDSPostureNodeWriter returns an RDSPostureNodeWriter backed by the given
// Executor and PostureExistenceReader. A batchSize of 0 or less uses
// DefaultBatchSize.
func NewRDSPostureNodeWriter(executor Executor, reader PostureExistenceReader, batchSize int) *RDSPostureNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &RDSPostureNodeWriter{executor: executor, reader: reader, batchSize: batchSize}
}

// WriteRDSPostureNodes stamps reducer-owned RDS posture properties onto
// existing CloudResource nodes using a batched MATCH+SET statement. The write is
// idempotent: retries update the same uid and never fabricate a missing node.
func (w *RDSPostureNodeWriter) WriteRDSPostureNodes(
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
		return fmt.Errorf("rds posture node writer executor is required")
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
		return fmt.Errorf("rds posture node writer: %w", err)
	}
	if len(existing) == 0 {
		return nil
	}

	stmts := buildBatchedStatements(canonicalRDSPostureUpdateCypher, existing, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseRDSPosture
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = rdsPostureNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			rdsPostureNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractRDSPostureNodes removes only reducer-owned RDS posture properties for
// the given scopes. It leaves CloudResource identity and non-RDS properties
// untouched.
func (w *RDSPostureNodeWriter) RetractRDSPostureNodes(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("rds posture node writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractRDSPosturePropertiesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseRDSPosture,
			StatementMetadataEntityLabelKey: rdsPostureNodeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				rdsPostureNodeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func (w *RDSPostureNodeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract runs retract statements sequentially through Execute, each in
// its own auto-commit transaction — never ExecuteGroup. On NornicDB v1.1.11 a
// retract inside a managed transaction can under-apply even as a single
// statement (#4367/#5128/#5146/#5152). RetractRDSPostureNodes routes through
// this so the RDS-posture node retract is never batched with a sibling write via
// ExecuteGroup.
func (w *RDSPostureNodeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
