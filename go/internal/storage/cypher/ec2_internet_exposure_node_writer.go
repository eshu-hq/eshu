// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const canonicalPhaseEC2InternetExposure = "ec2_internet_exposure"

const ec2InternetExposureNodeLabel = "CloudResource:EC2InternetExposure"

// canonicalEC2InternetExposureNodeUpsertCypher anchors with MERGE, not a bare
// MATCH. Issue #5652: on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11) a bare-MATCH-anchored UNWIND SET silently drops
// its write (statement reports success; the property is never persisted).
// MERGE is not a blind substitute for this writer's never-create contract —
// WriteEC2InternetExposureNodes only ever MERGEs a uid that
// filterRowsToExistingCloudResourceUIDs already confirmed exists via a
// separate read, so MERGE always matches and never creates. See
// posture_node_existence.go and
// docs/internal/evidence/5652-nornic-bare-match-writeloss.md.
const canonicalEC2InternetExposureNodeUpsertCypher = `UNWIND $rows AS row
MERGE (resource:CloudResource {uid: row.uid})
SET resource.ec2_internet_exposure_state = row.state,
    resource.ec2_internet_exposed = row.internet_exposed,
    resource.ec2_internet_exposure_reason = row.reason,
    resource.ec2_internet_exposure_scope_id = row.scope_id,
    resource.ec2_internet_exposure_generation_id = row.generation_id,
    resource.ec2_internet_exposure_evidence_source = row.evidence_source,
    resource.ec2_internet_exposure_source_fact_id = row.source_fact_id`

const retractEC2InternetExposureNodesCypher = `MATCH (resource:CloudResource)
WHERE resource.ec2_internet_exposure_scope_id IN $scope_ids
  AND resource.ec2_internet_exposure_evidence_source = $evidence_source
REMOVE resource.ec2_internet_exposure_state,
       resource.ec2_internet_exposed,
       resource.ec2_internet_exposure_reason,
       resource.ec2_internet_exposure_scope_id,
       resource.ec2_internet_exposure_generation_id,
       resource.ec2_internet_exposure_evidence_source,
       resource.ec2_internet_exposure_source_fact_id`

// EC2InternetExposureNodeWriter writes reducer-owned EC2 internet-exposure
// properties onto already-materialized CloudResource nodes. It never creates
// CloudResource nodes: WriteEC2InternetExposureNodes reads which candidate
// uids already exist first and drops rows for uids that do not, so a missing
// uid is a no-op before the write ever runs.
type EC2InternetExposureNodeWriter struct {
	executor  Executor
	reader    PostureExistenceReader
	batchSize int
}

// NewEC2InternetExposureNodeWriter returns an EC2InternetExposureNodeWriter
// backed by the given Executor and PostureExistenceReader. A batchSize of 0 or
// less uses DefaultBatchSize.
func NewEC2InternetExposureNodeWriter(executor Executor, reader PostureExistenceReader, batchSize int) *EC2InternetExposureNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &EC2InternetExposureNodeWriter{executor: executor, reader: reader, batchSize: batchSize}
}

// WriteEC2InternetExposureNodes sets reducer-owned exposure properties on
// existing EC2 CloudResource nodes. Unknown exposure rows carry
// internet_exposed=nil; Cypher treats that as property removal while preserving
// the explicit state=unknown property.
func (w *EC2InternetExposureNodeWriter) WriteEC2InternetExposureNodes(
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
		return fmt.Errorf("ec2 internet exposure node writer executor is required")
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
		return fmt.Errorf("ec2 internet exposure node writer: %w", err)
	}
	if len(existing) == 0 {
		return nil
	}

	stmts := buildBatchedStatements(canonicalEC2InternetExposureNodeUpsertCypher, existing, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseEC2InternetExposure
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = ec2InternetExposureNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"node_properties=%s rows=%d",
			ec2InternetExposureNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractEC2InternetExposureNodes removes reducer-owned exposure properties for
// the supplied scopes. It leaves CloudResource nodes and unrelated properties in
// place.
func (w *EC2InternetExposureNodeWriter) RetractEC2InternetExposureNodes(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 internet exposure node writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractEC2InternetExposureNodesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseEC2InternetExposure,
			StatementMetadataEntityLabelKey: ec2InternetExposureNodeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"node_properties=%s retract scopes=%d generation=%s",
				ec2InternetExposureNodeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func (w *EC2InternetExposureNodeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
// statement (#4367/#5128/#5146/#5152). RetractEC2InternetExposureNodes routes
// through this so the internet-exposure property retract is never batched with a
// sibling write via ExecuteGroup.
func (w *EC2InternetExposureNodeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
