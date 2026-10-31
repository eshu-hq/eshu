// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ecsBindingCensusEntityTypes is the closed entity set the workload↔ECS
// binding census reads: service declarations plus every context family
// the bounded resolver consumes (variables, locals, module calls,
// terragrunt stacks and inputs). Kept as a constant so the query-shape
// test derives from the shipped SQL instead of hand-copying it.
var ecsBindingCensusEntityTypes = []string{
	"TerraformResource",
	"TerraformModule",
	"TerraformVariable",
	"TerraformLocal",
	"TerragruntConfig",
	"TerragruntInput",
}

// ecsBindingCensusSQL lists active-generation IaC declarations
// corpus-wide with their full entity metadata. It mirrors the IaC
// inventory CTE's generation join (scope_generations status='active'
// leading into fact_records on scope+generation, served by the
// scope/generation keyset index) without caller authorization: the
// reducer materializes system truth and query surfaces enforce grants.
// entity_metadata carries the parser row verbatim (attributes,
// unknown_attributes, defaults, input values); a row cap bounds the
// read and the caller reports truncation visibly instead of binding
// on a silent subset.
const ecsBindingCensusSQL = `
SELECT fact.payload->>'repo_id' AS repo_id,
       fact.payload->>'relative_path' AS relative_path,
       fact.payload->>'entity_type' AS entity_type,
       fact.payload->>'entity_name' AS entity_name,
       fact.payload->'entity_metadata' AS entity_metadata
FROM scope_generations AS generation
JOIN fact_records AS fact
  ON fact.scope_id = generation.scope_id
 AND fact.generation_id = generation.generation_id
WHERE generation.status = 'active'
  AND fact.fact_kind = 'content_entity'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' IN (
    'TerraformResource',
    'TerraformModule',
    'TerraformVariable',
    'TerraformLocal',
    'TerragruntConfig',
    'TerragruntInput'
  )
ORDER BY repo_id, relative_path, entity_type, entity_name
LIMIT $1
`

// ECSBindingDeclarationCensusStore serves the reducer's global
// declaration census from active fact generations.
type ECSBindingDeclarationCensusStore struct {
	reader db.Queryer
	limit  int
}

// NewECSBindingDeclarationCensusStore binds the census to a query port.
// A non-positive limit selects the default census cap.
func NewECSBindingDeclarationCensusStore(reader db.Queryer, limit int) ECSBindingDeclarationCensusStore {
	if limit <= 0 {
		limit = reducer.ECSBindingCensusCap()
	}
	return ECSBindingDeclarationCensusStore{reader: reader, limit: limit}
}

// ListECSBindingDeclarations implements reducer.ECSBindingDeclarationCensus.
func (s ECSBindingDeclarationCensusStore) ListECSBindingDeclarations(ctx context.Context) (reducer.ECSBindingCensusResult, error) {
	if s.reader == nil {
		return reducer.ECSBindingCensusResult{}, fmt.Errorf("ECS binding census reader is required")
	}
	rows, err := s.reader.QueryContext(ctx, ecsBindingCensusSQL, s.limit+1)
	if err != nil {
		return reducer.ECSBindingCensusResult{}, fmt.Errorf("read ECS binding declaration census: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := reducer.ECSBindingCensusResult{}
	for rows.Next() {
		var decl reducer.ECSBindingDeclaration
		var metadata []byte
		var repoID, relativePath, entityType, entityName sql.NullString
		if err := rows.Scan(&repoID, &relativePath, &entityType, &entityName, &metadata); err != nil {
			return reducer.ECSBindingCensusResult{}, fmt.Errorf("scan ECS binding declaration census: %w", err)
		}
		if len(result.Declarations) >= s.limit {
			result.Truncated = true
			break
		}
		decl.RepoID = repoID.String
		decl.EntityType = entityType.String
		decl.EntityName = entityName.String
		decl.Path = relativePath.String
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &decl.Metadata); err != nil {
				return reducer.ECSBindingCensusResult{}, fmt.Errorf("decode ECS binding declaration metadata: %w", err)
			}
		}
		if decl.Metadata == nil {
			decl.Metadata = map[string]any{}
		}
		result.Declarations = append(result.Declarations, decl)
	}
	if err := rows.Err(); err != nil {
		return reducer.ECSBindingCensusResult{}, fmt.Errorf("iterate ECS binding declaration census: %w", err)
	}
	if err := rows.Close(); err != nil {
		return reducer.ECSBindingCensusResult{}, fmt.Errorf("close ECS binding declaration census: %w", err)
	}
	return result, nil
}
