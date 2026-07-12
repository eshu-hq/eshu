// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const canonicalPhaseIAMInstanceProfileRoleEdge = "iam_instance_profile_role_edge"

const iamInstanceProfileRoleEdgeLabel = "IAM_INSTANCE_PROFILE_HAS_ROLE"

var iamInstanceProfileRoleRelationshipVocabulary = map[string]struct{}{
	"HAS_ROLE": {},
}

const canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (profile:CloudResource {uid: row.profile_uid})
MATCH (role:CloudResource {uid: row.role_uid})
MERGE (profile)-[rel:%s]->(role)
SET rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const retractIAMInstanceProfileRoleEdgesCypher = `MATCH (:CloudResource)-[rel:HAS_ROLE]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// IAMInstanceProfileRoleEdgeWriter materializes resolved IAM instance-profile
// attachments into canonical HAS_ROLE edges between CloudResource nodes.
type IAMInstanceProfileRoleEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewIAMInstanceProfileRoleEdgeWriter returns an
// IAMInstanceProfileRoleEdgeWriter backed by the given Executor. A batchSize of
// 0 or less uses DefaultBatchSize.
func NewIAMInstanceProfileRoleEdgeWriter(executor Executor, batchSize int) *IAMInstanceProfileRoleEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &IAMInstanceProfileRoleEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteIAMInstanceProfileRoleEdges upserts HAS_ROLE edges using batched
// MATCH-MATCH-MERGE statements. The relationship type is accepted only from the
// closed single-member vocabulary before interpolation, preserving a static
// token MERGE and preventing relationship-type injection.
func (w *IAMInstanceProfileRoleEdgeWriter) WriteIAMInstanceProfileRoleEdges(
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
		return fmt.Errorf("iam instance-profile role edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateIAMInstanceProfileRoleRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	cypher := fmt.Sprintf(
		canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat,
		iamInstanceProfileRoleRelationshipType(),
	)
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseIAMInstanceProfileRoleEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = iamInstanceProfileRoleEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			iamInstanceProfileRoleEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractIAMInstanceProfileRoleEdges removes this reducer's HAS_ROLE edges for
// the given scopes before a fresh generation reprojects them.
func (w *IAMInstanceProfileRoleEdgeWriter) RetractIAMInstanceProfileRoleEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("iam instance-profile role edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractIAMInstanceProfileRoleEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseIAMInstanceProfileRoleEdge,
			StatementMetadataEntityLabelKey: iamInstanceProfileRoleEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				iamInstanceProfileRoleEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func validateIAMInstanceProfileRoleRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(
		row,
		"relationship_type",
		iamInstanceProfileRoleRelationshipVocabulary,
		"iam instance-profile role relationship_type",
	)
}

func iamInstanceProfileRoleRelationshipType() string {
	for token := range iamInstanceProfileRoleRelationshipVocabulary {
		return token
	}
	return ""
}

func (w *IAMInstanceProfileRoleEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract routes retract statements through sequential Execute calls,
// never ExecuteGroup. On the pinned NornicDB v1.1.11 a DELETE dispatched
// through ExecuteGroup / a managed transaction under-applies — even a single
// statement — while the identical statement run as an auto-commit transaction
// (Execute) deletes correctly. See
// docs/public/reference/nornicdb-pitfalls.md and
// CodeInterprocEvidenceWriter.dispatchRetract for the same rationale applied
// to the code-interproc evidence retract.
func (w *IAMInstanceProfileRoleEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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
