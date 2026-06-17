package cypher

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func (w *EdgeWriter) RetractEdges(
	ctx context.Context,
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("edge writer executor is required")
	}

	if domain == reducer.DomainCodeCalls {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmt := BuildRetractCodeCallEdgesByFilePath(filePaths, evidenceSource)
			return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
		}
	}

	if domain == reducer.DomainInheritanceEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmt := BuildRetractInheritanceEdgesByFilePath(filePaths, evidenceSource)
			return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
		}
	}

	if domain == reducer.DomainRationaleEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmt := BuildRetractRationaleEdgesByFilePath(filePaths, evidenceSource)
			return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
		}
	}

	if domain == reducer.DomainDocumentationEdges {
		deltaScope, hasDeltaScope, err := collectDocumentationDeltaScope(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			scopeIDs := collectRepoIDs(rows)
			stmts := buildDocumentationDeltaRetractStatements(scopeIDs, deltaScope, evidenceSource)
			return w.executeDocumentationRetractStatements(ctx, stmts)
		}
	}

	repoIDs := collectRepoIDs(rows)
	if domain == reducer.DomainSQLRelationships {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmts := BuildRetractSQLRelationshipEdgeStatementsByFilePath(filePaths, evidenceSource)
			return w.executeSQLRelationshipRetractStatements(ctx, stmts)
		}
		if ge, ok := w.executor.(GroupExecutor); ok {
			stmts := BuildRetractSQLRelationshipEdgeStatements(repoIDs, evidenceSource)
			return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
		}
	}
	if domain == reducer.DomainRepoDependency {
		stmts := []Statement{
			{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoRelationshipAndRunsOnEdgesCypher,
				Parameters: map[string]any{
					"repo_ids":        repoIDs,
					"evidence_source": evidenceSource,
				},
			},
			{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoEvidenceArtifactsCypher,
				Parameters: map[string]any{
					"repo_ids":        repoIDs,
					"evidence_source": evidenceSource,
				},
			},
		}
		if ge, ok := w.executor.(GroupExecutor); ok {
			return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
		}
		for _, stmt := range stmts {
			if err := w.executor.Execute(ctx, stmt); err != nil {
				return WrapRetryableNeo4jError(err)
			}
		}
		return nil
	}

	stmt, err := buildRetractStatement(domain, repoIDs, evidenceSource)
	if err != nil {
		return err
	}

	return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
}

func (w *EdgeWriter) executeSQLRelationshipRetractStatements(ctx context.Context, stmts []Statement) error {
	if ge, ok := w.executor.(GroupExecutor); ok {
		return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

func (w *EdgeWriter) executeDocumentationRetractStatements(ctx context.Context, stmts []Statement) error {
	if ge, ok := w.executor.(GroupExecutor); ok {
		return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

func buildRetractStatement(
	domain string,
	repoIDs []string,
	evidenceSource string,
) (Statement, error) {
	switch domain {
	case reducer.DomainPlatformInfra:
		return BuildRetractInfrastructurePlatformEdges(repoIDs, evidenceSource), nil
	case reducer.DomainRepoDependency:
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractRepoRelationshipAndRunsOnEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		}, nil
	case reducer.DomainWorkloadDependency:
		return BuildRetractWorkloadDependencyEdges(repoIDs, evidenceSource), nil
	case reducer.DomainCodeCalls:
		return BuildRetractCodeCallEdges(repoIDs, evidenceSource), nil
	case reducer.DomainInheritanceEdges:
		return BuildRetractInheritanceEdges(repoIDs, evidenceSource), nil
	case reducer.DomainDocumentationEdges:
		return BuildRetractDocumentationEdges(repoIDs, evidenceSource), nil
	case reducer.DomainRationaleEdges:
		return BuildRetractRationaleEdges(repoIDs, evidenceSource), nil
	case reducer.DomainSQLRelationships:
		return BuildRetractSQLRelationshipEdges(repoIDs, evidenceSource), nil
	case reducer.DomainDeployableUnitEdges:
		return BuildRetractDeployableUnitCorrelationEdges(repoIDs, evidenceSource), nil
	case reducer.DomainHandlesRoute:
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractHandlesRouteEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		}, nil
	default:
		return Statement{}, fmt.Errorf("unsupported domain for retract: %q", domain)
	}
}

func collectRepoIDs(rows []reducer.SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var result []string
	for _, row := range rows {
		repoID := row.RepositoryID
		if repoID == "" {
			repoID = payloadString(row.Payload, "repo_id")
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		result = append(result, repoID)
	}
	return result
}

func collectDeltaFilePaths(rows []reducer.SharedProjectionIntentRow) ([]string, bool, error) {
	seen := make(map[string]struct{})
	hasDeltaScope := false
	var filePaths []string
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		rowFilePaths := payloadStringSlice(row.Payload, "delta_file_paths")
		if len(rowFilePaths) == 0 {
			return nil, true, fmt.Errorf("delta retract requires delta_file_paths")
		}
		for _, filePath := range rowFilePaths {
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				continue
			}
			if _, ok := seen[filePath]; ok {
				continue
			}
			seen[filePath] = struct{}{}
			filePaths = append(filePaths, filePath)
		}
	}
	if hasDeltaScope && len(filePaths) == 0 {
		return nil, true, fmt.Errorf("delta retract requires delta_file_paths")
	}
	sort.Strings(filePaths)
	return filePaths, hasDeltaScope, nil
}

type documentationRetractScope struct {
	documentIDs []string
	sectionUIDs []string
}

func collectDocumentationDeltaScope(rows []reducer.SharedProjectionIntentRow) (documentationRetractScope, bool, error) {
	seenDocuments := make(map[string]struct{})
	seenSections := make(map[string]struct{})
	hasDeltaScope := false
	scope := documentationRetractScope{}
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		rowDocumentIDs := payloadStringSlice(row.Payload, "document_ids")
		for _, documentID := range rowDocumentIDs {
			documentID = strings.TrimSpace(documentID)
			if documentID == "" {
				continue
			}
			if _, ok := seenDocuments[documentID]; ok {
				continue
			}
			seenDocuments[documentID] = struct{}{}
			scope.documentIDs = append(scope.documentIDs, documentID)
		}
		for _, sectionUID := range payloadStringSlice(row.Payload, "section_uids") {
			sectionUID = strings.TrimSpace(sectionUID)
			if sectionUID == "" {
				continue
			}
			if _, ok := seenSections[sectionUID]; ok {
				continue
			}
			seenSections[sectionUID] = struct{}{}
			scope.sectionUIDs = append(scope.sectionUIDs, sectionUID)
		}
	}
	if hasDeltaScope && len(scope.documentIDs) == 0 && len(scope.sectionUIDs) == 0 {
		return documentationRetractScope{}, true, fmt.Errorf("documentation delta retract requires document_ids or section_uids")
	}
	sort.Strings(scope.documentIDs)
	sort.Strings(scope.sectionUIDs)
	return scope, hasDeltaScope, nil
}

func buildDocumentationDeltaRetractStatements(
	scopeIDs []string,
	deltaScope documentationRetractScope,
	evidenceSource string,
) []Statement {
	stmts := make([]Statement, 0, 2)
	if len(deltaScope.sectionUIDs) > 0 {
		stmts = append(stmts, BuildRetractDocumentationEdgesBySectionUID(
			scopeIDs,
			deltaScope.sectionUIDs,
			evidenceSource,
		))
	}
	if len(deltaScope.documentIDs) > 0 {
		stmts = append(stmts, BuildRetractDocumentationEdgesByDocumentID(
			scopeIDs,
			deltaScope.documentIDs,
			evidenceSource,
		))
	}
	return stmts
}

// copyRepoRelationshipMetadata preserves durable evidence pointers on graph
// edge writes while keeping the full evidence payload in Postgres.
func copyRepoRelationshipMetadata(rowMap map[string]any, payload map[string]any, rowGenerationID string) {
	rowMap["resolved_id"] = payloadString(payload, "resolved_id")
	generationID := payloadString(payload, "generation_id")
	if generationID == "" {
		generationID = rowGenerationID
	}
	rowMap["generation_id"] = generationID
	rowMap["evidence_count"] = payloadInt(payload, "evidence_count")
	rowMap["evidence_kinds"] = payloadStringSlice(payload, "evidence_kinds")
	rowMap["resolution_source"] = payloadString(payload, "resolution_source")
	rowMap["confidence"] = repoRelationshipConfidence(payloadFloat(payload, "confidence"))
	rowMap["rationale"] = payloadString(payload, "rationale")
}

// repoEvidenceArtifactRowsFromIntent builds bounded graph nodes from reducer
// evidence summaries while preserving raw detail ownership in Postgres.
func repoEvidenceArtifactRowsFromIntent(
	row reducer.SharedProjectionIntentRow,
	evidenceSource string,
) []map[string]any {
	payload := row.Payload
	repoID := payloadString(payload, "repo_id")
	targetRepoID := payloadString(payload, "target_repo_id")
	if repoID == "" || targetRepoID == "" {
		return nil
	}
	artifacts := payloadMapSlice(payload, "evidence_artifacts")
	if len(artifacts) == 0 {
		return nil
	}

	relationshipType := payloadString(payload, "relationship_type")
	resolvedID := payloadString(payload, "resolved_id")
	generationID := payloadString(payload, "generation_id")
	if generationID == "" {
		generationID = row.GenerationID
	}
	rows := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		evidenceKind := payloadString(artifact, "evidence_kind")
		path := payloadString(artifact, "path")
		matchedValue := payloadString(artifact, "matched_value")
		name := path
		if name == "" {
			name = evidenceKind
		}
		artifactID := repoEvidenceArtifactID(resolvedID, evidenceKind, path, matchedValue)
		rows = append(rows, map[string]any{
			"artifact_id":           artifactID,
			"name":                  name,
			"repo_id":               repoID,
			"target_repo_id":        targetRepoID,
			"relationship_type":     relationshipType,
			"resolved_id":           resolvedID,
			"generation_id":         generationID,
			"evidence_kind":         evidenceKind,
			"artifact_family":       payloadString(artifact, "artifact_family"),
			"path":                  path,
			"extractor":             payloadString(artifact, "extractor"),
			"environment":           payloadString(artifact, "environment"),
			"runtime_platform_kind": payloadString(artifact, "runtime_platform_kind"),
			"matched_alias":         payloadString(artifact, "matched_alias"),
			"matched_value":         matchedValue,
			"confidence":            payloadFloat(artifact, "confidence"),
			"evidence_source":       evidenceSource,
		})
	}
	return rows
}

func repoEvidenceArtifactID(resolvedID string, evidenceKind string, path string, matchedValue string) string {
	hash := sha1.Sum([]byte(strings.Join([]string{resolvedID, evidenceKind, path, matchedValue}, "\x00")))
	return "evidence-artifact:" + hex.EncodeToString(hash[:8])
}
