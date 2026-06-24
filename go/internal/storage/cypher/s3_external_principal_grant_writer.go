// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const (
	canonicalPhaseS3ExternalPrincipalGrant = "s3_external_principal_grant"
	s3ExternalPrincipalGrantLabel          = "ExternalPrincipal"
)

var s3ExternalPrincipalGrantRelationshipVocabulary = map[string]struct{}{
	"GRANTS_ACCESS_TO": {},
}

const canonicalS3ExternalPrincipalGrantUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MERGE (principal:ExternalPrincipal {uid: row.principal_uid})
SET principal.id = row.principal_uid,
    principal.principal_kind = row.principal_kind,
    principal.principal_value = row.principal_value,
    principal.principal_account_id = CASE WHEN row.principal_account_id <> '' THEN row.principal_account_id ELSE principal.principal_account_id END,
    principal.principal_partition = CASE WHEN row.principal_partition <> '' THEN row.principal_partition ELSE principal.principal_partition END,
    principal.principal_service = CASE WHEN row.principal_service <> '' THEN row.principal_service ELSE principal.principal_service END,
    principal.scope_id = row.scope_id,
    principal.generation_id = row.generation_id,
    principal.evidence_source = row.evidence_source
MERGE (source)-[rel:%s]->(principal)
SET rel.grant_outcome = row.grant_outcome,
    rel.is_public = row.is_public,
    rel.is_cross_account = row.is_cross_account,
    rel.is_service_principal = row.is_service_principal,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const retractS3ExternalPrincipalGrantEdgesCypher = `MATCH (:CloudResource)-[rel:GRANTS_ACCESS_TO]->(:ExternalPrincipal)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// S3ExternalPrincipalGrantWriter materializes metadata-only S3 bucket-policy
// external-principal grant facts into ExternalPrincipal nodes and
// GRANTS_ACCESS_TO edges. It never creates CloudResource nodes; missing source
// buckets are no-ops at the MATCH.
type S3ExternalPrincipalGrantWriter struct {
	executor  Executor
	batchSize int
}

// NewS3ExternalPrincipalGrantWriter returns an S3ExternalPrincipalGrantWriter
// backed by the given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewS3ExternalPrincipalGrantWriter(executor Executor, batchSize int) *S3ExternalPrincipalGrantWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &S3ExternalPrincipalGrantWriter{executor: executor, batchSize: batchSize}
}

// WriteS3ExternalPrincipalGrants upserts ExternalPrincipal nodes and static
// GRANTS_ACCESS_TO edges for already-resolved S3 bucket rows. The relationship
// type is validated against a closed vocabulary before interpolation.
func (w *S3ExternalPrincipalGrantWriter) WriteS3ExternalPrincipalGrants(
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
		return fmt.Errorf("s3 external-principal grant writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateS3ExternalPrincipalGrantRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	cypher := fmt.Sprintf(canonicalS3ExternalPrincipalGrantUpsertCypherFormat, s3ExternalPrincipalGrantRelationshipType())
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseS3ExternalPrincipalGrant
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = s3ExternalPrincipalGrantLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s edge=GRANTS_ACCESS_TO rows=%d",
			s3ExternalPrincipalGrantLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractS3ExternalPrincipalGrants removes this reducer's GRANTS_ACCESS_TO edges
// for the given scopes. ExternalPrincipal nodes are left in place because they
// are global identities and may be referenced by other scopes.
func (w *S3ExternalPrincipalGrantWriter) RetractS3ExternalPrincipalGrants(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("s3 external-principal grant writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractS3ExternalPrincipalGrantEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseS3ExternalPrincipalGrant,
			StatementMetadataEntityLabelKey: s3ExternalPrincipalGrantLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				s3ExternalPrincipalGrantLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

func validateS3ExternalPrincipalGrantRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "relationship_type", s3ExternalPrincipalGrantRelationshipVocabulary, "s3 external-principal grant relationship_type")
}

func s3ExternalPrincipalGrantRelationshipType() string {
	for token := range s3ExternalPrincipalGrantRelationshipVocabulary {
		return token
	}
	return ""
}

func (w *S3ExternalPrincipalGrantWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
