// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 stayer for #6060: methods on the root ContentReader must live in package query; the shared read model moved to querycontract.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryRelationshipReadModel is the Postgres read-model fast path for a
// repository's resolved relationship rows and derived consumers, hydrated
// from resolved_relationships so API reads avoid the incoming-fanout graph
// traversal repository_context.go otherwise runs as three separate Neo4j
// queries (queryRepoDependencies, queryRepoRelationshipOverview,
// queryRepoConsumers).
//
// It is an alias onto querycontract so the shared ContentStore double can
// name it from outside this package (#6060). See the querycontract
// declaration for the Available fallback obligation.
type RepositoryRelationshipReadModel = querycontract.RepositoryRelationshipReadModel

type repositoryRelationshipReadModelStore interface {
	RepositoryRelationshipReadModel(context.Context, string) (RepositoryRelationshipReadModel, error)
}

// loadRepositoryRelationshipReadModel returns resolved relationship truth from
// the Postgres read model when the content store can provide it. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func loadRepositoryRelationshipReadModel(ctx context.Context, content ContentStore, repoID string) *RepositoryRelationshipReadModel {
	return querycontract.LoadRepositoryRelationshipReadModel(ctx, content, repoID)
}

// RepositoryRelationshipReadModel hydrates repository relationship rows from
// resolved_relationships so API reads avoid expensive graph incoming fanout.
func (cr *ContentReader) RepositoryRelationshipReadModel(ctx context.Context, repoID string) (RepositoryRelationshipReadModel, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return RepositoryRelationshipReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, repositoryRelationshipReadModelSQL, repoID)
	if err != nil {
		return RepositoryRelationshipReadModel{}, fmt.Errorf("query repository relationship read model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	relationships := make([]map[string]any, 0)
	consumers := make([]map[string]any, 0)
	seenConsumers := map[string]struct{}{}
	for rows.Next() {
		row, err := scanRepositoryRelationshipReadModelRow(rows)
		if err != nil {
			return RepositoryRelationshipReadModel{}, err
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
		return RepositoryRelationshipReadModel{}, fmt.Errorf("iterate repository relationship read model: %w", err)
	}
	return RepositoryRelationshipReadModel{
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
