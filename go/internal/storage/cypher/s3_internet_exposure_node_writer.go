// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const canonicalPhaseS3InternetExposure = "s3_internet_exposure"

const s3InternetExposureNodeLabel = "CloudResource:S3InternetExposure"

// canonicalS3InternetExposureNodeUpsertCypher anchors with MERGE, not a bare
// MATCH. Issue #5652: on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11) a bare-MATCH-anchored UNWIND SET silently drops
// its write (statement reports success; the property is never persisted).
// MERGE is not a blind substitute for this writer's never-create contract —
// WriteS3InternetExposureNodes only ever MERGEs a uid that
// filterRowsToExistingCloudResourceUIDs already confirmed exists via a
// separate read, so MERGE always matches and never creates. See
// posture_node_existence.go and
// docs/internal/evidence/5652-nornic-bare-match-writeloss.md.
const canonicalS3InternetExposureNodeUpsertCypher = `UNWIND $rows AS row
MERGE (resource:CloudResource {uid: row.uid})
SET resource.s3_internet_exposure_state = row.state,
    resource.s3_internet_exposed = row.internet_exposed,
    resource.s3_internet_exposure_reason = row.reason,
    resource.s3_internet_exposure_scope_id = row.scope_id,
    resource.s3_internet_exposure_generation_id = row.generation_id,
    resource.s3_internet_exposure_evidence_source = row.evidence_source,
    resource.s3_internet_exposure_source_fact_id = row.source_fact_id`

const retractS3InternetExposureNodesCypher = `MATCH (resource:CloudResource)
WHERE resource.s3_internet_exposure_scope_id IN $scope_ids
  AND resource.s3_internet_exposure_evidence_source = $evidence_source
REMOVE resource.s3_internet_exposure_state,
       resource.s3_internet_exposed,
       resource.s3_internet_exposure_reason,
       resource.s3_internet_exposure_scope_id,
       resource.s3_internet_exposure_generation_id,
       resource.s3_internet_exposure_evidence_source,
       resource.s3_internet_exposure_source_fact_id`

// S3InternetExposureNodeWriter writes reducer-owned S3 internet-exposure
// properties onto already-materialized CloudResource nodes. It never creates
// CloudResource nodes: WriteS3InternetExposureNodes reads which candidate
// uids already exist first and drops rows for uids that do not, so a missing
// uid is a no-op before the write ever runs.
type S3InternetExposureNodeWriter struct {
	executor  Executor
	reader    PostureExistenceReader
	batchSize int
}

// NewS3InternetExposureNodeWriter returns an S3InternetExposureNodeWriter
// backed by the given Executor and PostureExistenceReader. A batchSize of 0 or
// less uses DefaultBatchSize (500).
func NewS3InternetExposureNodeWriter(executor Executor, reader PostureExistenceReader, batchSize int) *S3InternetExposureNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &S3InternetExposureNodeWriter{executor: executor, reader: reader, batchSize: batchSize}
}

// WriteS3InternetExposureNodes sets reducer-owned exposure properties on
// existing CloudResource nodes. The writer injects scope/generation/evidence
// metadata into each row so retractions can remove only properties owned by this
// reducer. Unknown exposure rows carry internet_exposed=nil; Cypher treats that
// as property removal while preserving the explicit state=unknown property.
func (w *S3InternetExposureNodeWriter) WriteS3InternetExposureNodes(
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
		return fmt.Errorf("s3 internet exposure node writer executor is required")
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
		return fmt.Errorf("s3 internet exposure node writer: %w", err)
	}
	if len(existing) == 0 {
		return nil
	}

	stmts := buildBatchedStatements(canonicalS3InternetExposureNodeUpsertCypher, existing, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseS3InternetExposure
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = s3InternetExposureNodeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"node_properties=%s rows=%d",
			s3InternetExposureNodeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractS3InternetExposureNodes removes reducer-owned exposure properties for
// the supplied scopes. It leaves CloudResource nodes and unrelated properties in
// place.
func (w *S3InternetExposureNodeWriter) RetractS3InternetExposureNodes(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("s3 internet exposure node writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractS3InternetExposureNodesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseS3InternetExposure,
			StatementMetadataEntityLabelKey: s3InternetExposureNodeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"node_properties=%s retract scopes=%d generation=%s",
				s3InternetExposureNodeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func (w *S3InternetExposureNodeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
// statement (#4367/#5128/#5146/#5152). RetractS3InternetExposureNodes routes
// through this so the S3 internet-exposure property retract is never batched with
// a sibling write via ExecuteGroup.
func (w *S3InternetExposureNodeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
