// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type repositoryRelationshipReadModel struct {
	Available     bool
	Relationships []map[string]any
	Consumers     []map[string]any
}

type repositoryRelationshipReadModelStore interface {
	repositoryRelationshipReadModel(context.Context, string) (repositoryRelationshipReadModel, error)
}

// loadRepositoryRelationshipReadModel returns resolved relationship truth from
// the Postgres read model when the content store can provide it.
func loadRepositoryRelationshipReadModel(ctx context.Context, content ContentStore, repoID string) *repositoryRelationshipReadModel {
	store, ok := content.(repositoryRelationshipReadModelStore)
	if !ok || repoID == "" {
		return nil
	}
	readModel, err := store.repositoryRelationshipReadModel(ctx, repoID)
	if err != nil || !readModel.Available {
		return nil
	}
	return &readModel
}

// repositoryRelationshipReadModel hydrates repository relationship rows from
// resolved_relationships so API reads avoid expensive graph incoming fanout.
func (cr *ContentReader) repositoryRelationshipReadModel(ctx context.Context, repoID string) (repositoryRelationshipReadModel, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return repositoryRelationshipReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, repositoryRelationshipReadModelSQL, repoID)
	if err != nil {
		return repositoryRelationshipReadModel{}, fmt.Errorf("query repository relationship read model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	relationships := make([]map[string]any, 0)
	consumers := make([]map[string]any, 0)
	seenConsumers := map[string]struct{}{}
	for rows.Next() {
		row, err := scanRepositoryRelationshipReadModelRow(rows)
		if err != nil {
			return repositoryRelationshipReadModel{}, err
		}
		relationships = append(relationships, row)
		if StringVal(row, "direction") != "incoming" {
			continue
		}
		sourceID := StringVal(row, "source_id")
		sourceName := StringVal(row, "source_name")
		if sourceID == "" && sourceName == "" {
			continue
		}
		key := sourceID + "|" + sourceName
		if _, ok := seenConsumers[key]; ok {
			continue
		}
		seenConsumers[key] = struct{}{}
		consumers = append(consumers, map[string]any{
			"id":   sourceID,
			"name": sourceName,
		})
	}
	if err := rows.Err(); err != nil {
		return repositoryRelationshipReadModel{}, fmt.Errorf("iterate repository relationship read model: %w", err)
	}
	return repositoryRelationshipReadModel{
		Available:     len(relationships) > 0,
		Relationships: relationships,
		Consumers:     consumers,
	}, nil
}

const repositoryRelationshipReadModelSQL = `
WITH scoped_relationships AS (
	SELECT 'outgoing' AS direction, r.*
	FROM resolved_relationships AS r
	JOIN relationship_generations AS g
	  ON g.generation_id = r.generation_id
	WHERE g.status = 'active'
	  AND r.source_repo_id = $1
	  AND r.relationship_type IN (
		'DEPENDS_ON',
		'USES_MODULE',
		'DEPLOYS_FROM',
		'DISCOVERS_CONFIG_IN',
		'PROVISIONS_DEPENDENCY_FOR',
		'READS_CONFIG_FROM',
		'RUNS_ON',
		'CORRELATES_DEPLOYABLE_UNIT'
	  )
	UNION ALL
	SELECT 'incoming' AS direction, r.*
	FROM resolved_relationships AS r
	JOIN relationship_generations AS g
	  ON g.generation_id = r.generation_id
	WHERE g.status = 'active'
	  AND r.target_repo_id = $1
	  AND r.relationship_type IN (
		'DEPENDS_ON',
		'USES_MODULE',
		'DEPLOYS_FROM',
		'DISCOVERS_CONFIG_IN',
		'PROVISIONS_DEPENDENCY_FOR',
		'READS_CONFIG_FROM',
		'RUNS_ON',
		'CORRELATES_DEPLOYABLE_UNIT'
	  )
)
SELECT r.direction,
       r.relationship_type,
       COALESCE(r.source_repo_id, '') AS source_repo_id,
       COALESCE(source_scope.name, r.source_repo_id, '') AS source_name,
       COALESCE(r.target_repo_id, '') AS target_repo_id,
       COALESCE(target_scope.name, r.target_repo_id, '') AS target_name,
       r.resolved_id,
       r.generation_id,
       r.confidence,
       r.evidence_count,
       r.rationale,
       r.resolution_source,
       r.details
FROM scoped_relationships AS r
LEFT JOIN LATERAL (
	SELECT COALESCE(
		payload->>'name',
		payload->>'repo_name',
		payload->>'repo_slug',
		source_key,
		scope_id
	) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (
		scope_id = r.source_repo_id OR
		source_key = r.source_repo_id OR
		payload->>'repo_id' = r.source_repo_id OR
		payload->>'id' = r.source_repo_id
	  )
	ORDER BY scope_id
	LIMIT 1
) AS source_scope ON true
LEFT JOIN LATERAL (
	SELECT COALESCE(
		payload->>'name',
		payload->>'repo_name',
		payload->>'repo_slug',
		source_key,
		scope_id
	) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (
		scope_id = r.target_repo_id OR
		source_key = r.target_repo_id OR
		payload->>'repo_id' = r.target_repo_id OR
		payload->>'id' = r.target_repo_id
	  )
	ORDER BY scope_id
	LIMIT 1
) AS target_scope ON true
ORDER BY r.direction, r.relationship_type, source_name, target_name, r.resolved_id
`

// scanRepositoryRelationshipReadModelRow converts one SQL row into the same
// relationship shape returned by graph-backed repository queries.
func scanRepositoryRelationshipReadModelRow(rows *sql.Rows) (map[string]any, error) {
	var (
		direction        string
		relationshipType string
		sourceID         string
		sourceName       string
		targetID         string
		targetName       string
		resolvedID       string
		generationID     string
		confidence       float64
		evidenceCount    int64
		rationale        string
		resolutionSource string
		detailsRaw       []byte
	)
	if err := rows.Scan(
		&direction,
		&relationshipType,
		&sourceID,
		&sourceName,
		&targetID,
		&targetName,
		&resolvedID,
		&generationID,
		&confidence,
		&evidenceCount,
		&rationale,
		&resolutionSource,
		&detailsRaw,
	); err != nil {
		return nil, fmt.Errorf("scan repository relationship read model: %w", err)
	}

	details := map[string]any{}
	if len(detailsRaw) > 0 {
		if err := json.Unmarshal(detailsRaw, &details); err != nil {
			return nil, fmt.Errorf("decode repository relationship details: %w", err)
		}
	}
	row := map[string]any{
		"direction":         direction,
		"type":              relationshipType,
		"source_id":         sourceID,
		"source_name":       sourceName,
		"target_id":         targetID,
		"target_name":       targetName,
		"resolved_id":       resolvedID,
		"generation_id":     generationID,
		"confidence":        confidence,
		"evidence_count":    int(evidenceCount),
		"rationale":         rationale,
		"resolution_source": resolutionSource,
	}
	evidenceKinds := repositoryRelationshipEvidenceKinds(details)
	if len(evidenceKinds) > 0 {
		row["evidence_kinds"] = evidenceKinds
	}
	if evidenceType := repositoryRelationshipEvidenceType(details, evidenceKinds); evidenceType != "" {
		row["evidence_type"] = evidenceType
	}
	addRelationshipConfidenceBasis(row)
	return row, nil
}

// repositoryRelationshipEvidenceKinds normalizes detail JSON into a stable
// string slice for response evidence pointers.
func repositoryRelationshipEvidenceKinds(details map[string]any) []string {
	if len(details) == 0 {
		return nil
	}
	if kinds := repositoryRelationshipStringSliceFromAny(details["evidence_kinds"]); len(kinds) > 0 {
		return kinds
	}
	preview, ok := details["evidence_preview"].([]any)
	if !ok {
		return nil
	}
	kinds := make([]string, 0, len(preview))
	for _, item := range preview {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind := strings.TrimSpace(StringVal(row, "kind")); kind != "" {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// repositoryRelationshipEvidenceType keeps Postgres-derived rows in parity
// with graph edges, whose evidence_type is a normalized evidence kind.
func repositoryRelationshipEvidenceType(details map[string]any, evidenceKinds []string) string {
	if len(details) > 0 {
		if evidenceType := strings.TrimSpace(StringVal(details, "evidence_type")); evidenceType != "" {
			return evidenceType
		}
	}
	if len(evidenceKinds) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(evidenceKinds[0]))
}

// stringSliceFromAny extracts strings from decoded JSON array values.
func repositoryRelationshipStringSliceFromAny(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}

// filterRepositoryRelationshipReadModelForAccess binds every related-repository
// endpoint carried by the read-model relationship rows and consumers to the
// caller's grant (#5167 W3 P0, fourth vector). The Postgres read-model path is
// the production-primary source for GET /repositories/{id}/context's
// relationships, relationship_overview, and consumers -- the read-model SQL
// anchors only on source_repo_id/target_repo_id = $anchor and applies no
// predicate to the FAR endpoint, and the deployable-unit merge
// (mergeRepositoryDeployableUnitRelationships) folds in graph rows through the
// unfiltered inner queryRepoRelationshipOverviewDirection. Filtering the merged
// read model here, before repository_context.go derives any result[] field from
// it, closes all three emit sites at once: relationship_overview and the
// legacy dependencies both derive from the filtered Relationships (anchor-aware
// -- the grant-verified anchor endpoint stays, the far endpoint must be in
// grant), and Consumers are filtered by their sole repository id. Deny-by-
// default when scoped; an all-scopes/shared/admin caller is unaffected
// (returned unchanged), so non-scoped callers see no regression. Available is
// carried through unchanged: an in-grant read model stays authoritative even
// when every cross-tenant row is dropped, so the handler does not fall back to
// the graph and re-run the (already grant-bound) helpers.
func filterRepositoryRelationshipReadModelForAccess(
	readModel *repositoryRelationshipReadModel,
	anchorRepoID string,
	access repositoryAccessFilter,
) *repositoryRelationshipReadModel {
	if readModel == nil || !access.scoped() {
		return readModel
	}
	return &repositoryRelationshipReadModel{
		Available:     readModel.Available,
		Relationships: filterRepoRelationshipOverviewRowsForAccess(readModel.Relationships, anchorRepoID, access),
		Consumers:     filterRepoRelationshipTargetRowsForAccess(readModel.Consumers, "id", access),
	}
}

// repositoryReadModelDependencies returns outgoing rows in the legacy
// repository dependency shape.
func repositoryReadModelDependencies(readModel *repositoryRelationshipReadModel) []map[string]any {
	if readModel == nil {
		return nil
	}
	dependencies := make([]map[string]any, 0, len(readModel.Relationships))
	for _, row := range readModel.Relationships {
		if StringVal(row, "direction") != "outgoing" {
			continue
		}
		dependency := map[string]any{
			"type":        StringVal(row, "type"),
			"target_name": StringVal(row, "target_name"),
			"target_id":   StringVal(row, "target_id"),
		}
		if evidenceType := StringVal(row, "evidence_type"); evidenceType != "" {
			dependency["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(dependency, row)
		dependencies = append(dependencies, dependency)
	}
	return dependencies
}
