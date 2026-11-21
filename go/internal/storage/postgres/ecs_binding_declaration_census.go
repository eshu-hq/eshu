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

// ecsBindingCensusSQL reads the filtered declaration closure the bounded
// ECS resolver consumes, complete by construction (#11084): every ECS
// service declaration corpus-wide (the only rows that become claims,
// and the only rows the uniqueness vetoes scan) plus the context rows
// the resolver reads around them — same-directory variables and locals,
// and same-repo module calls, terragrunt stacks, and terragrunt inputs
// (bridges are resolved in Go; the census keeps the whole repo's
// bridge rows so no SQL path-cleaning semantics can drop one).
// The legs run as one statement (one snapshot) driven by the ECS
// declaration set, so no cross-statement generation skew can split the
// closure. Directory matching renders relative_path with the same
// mapping the resolver uses (path.Dir: "main.tf" lives in "."); both
// sides of the join apply it, and the parser emits clean relative
// paths, so a mismatch can only fail closed on the unknown reference,
// never bind. It mirrors the IaC inventory CTE's generation join
// (scope_generations status='active' leading into fact_records on
// scope+generation, served by the scope/generation keyset index)
// without caller authorization: the reducer materializes system truth
// and query surfaces enforce grants. entity_metadata carries the parser
// row verbatim (attributes, unknown_attributes, defaults, input
// values). A safety ceiling bounds the read; hitting it marks the
// census incomplete, which blocks declaration binds for that
// extraction instead of binding on a silent subset.
const ecsBindingCensusSQL = `
WITH c1 AS MATERIALIZED (
  SELECT fact.scope_id AS scope_id, fact.generation_id AS generation_id,
         fact.payload->>'repo_id' AS repo_id,
         CASE WHEN fact.payload->>'relative_path' LIKE '%/%'
              THEN regexp_replace(fact.payload->>'relative_path', '/[^/]*$', '')
              ELSE '.' END AS dir,
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
    AND fact.payload->>'entity_type' = 'TerraformResource'
    AND fact.payload->'entity_metadata'->>'resource_type' = 'aws_ecs_service'
),
need_scope AS (SELECT DISTINCT scope_id, generation_id FROM c1),
need_dir AS (SELECT DISTINCT repo_id, dir FROM c1),
need_repo AS (SELECT DISTINCT repo_id FROM c1)
SELECT repo_id, relative_path, entity_type, entity_name, entity_metadata FROM c1
UNION ALL
SELECT fact.payload->>'repo_id', fact.payload->>'relative_path',
       fact.payload->>'entity_type', fact.payload->>'entity_name',
       fact.payload->'entity_metadata'
FROM need_scope
JOIN fact_records AS fact
  ON fact.scope_id = need_scope.scope_id
 AND fact.generation_id = need_scope.generation_id
JOIN need_dir
  ON need_dir.repo_id = fact.payload->>'repo_id'
 AND need_dir.dir = CASE WHEN fact.payload->>'relative_path' LIKE '%/%'
                         THEN regexp_replace(fact.payload->>'relative_path', '/[^/]*$', '')
                         ELSE '.' END
WHERE fact.fact_kind = 'content_entity'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' IN ('TerraformVariable', 'TerraformLocal')
UNION ALL
SELECT fact.payload->>'repo_id', fact.payload->>'relative_path',
       fact.payload->>'entity_type', fact.payload->>'entity_name',
       fact.payload->'entity_metadata'
FROM need_scope
JOIN fact_records AS fact
  ON fact.scope_id = need_scope.scope_id
 AND fact.generation_id = need_scope.generation_id
JOIN need_repo
  ON need_repo.repo_id = fact.payload->>'repo_id'
WHERE fact.fact_kind = 'content_entity'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' IN ('TerraformModule', 'TerragruntConfig', 'TerragruntInput')
ORDER BY 1, 2, 3, 4
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
	// Complete by construction: the single statement reads under one
	// snapshot, so reaching the end of the rows means the whole
	// closure was seen. Hitting the ceiling means the closure may be
	// larger than the read: report incomplete with no declarations so
	// the reducer blocks declaration binds for that extraction
	// (#11084), never binds on the retained rows.
	result := reducer.ECSBindingCensusResult{Complete: true}
	for rows.Next() {
		var decl reducer.ECSBindingDeclaration
		var metadata []byte
		var repoID, relativePath, entityType, entityName sql.NullString
		if err := rows.Scan(&repoID, &relativePath, &entityType, &entityName, &metadata); err != nil {
			return reducer.ECSBindingCensusResult{}, fmt.Errorf("scan ECS binding declaration census: %w", err)
		}
		if len(result.Declarations) >= s.limit {
			// Ceiling hit: drop the retained prefix and report
			// incomplete (zero value). A later rows.Err still fails
			// the read below: a DB error fails the intent, while a
			// clean ceiling hit blocks binds and succeeds it.
			result = reducer.ECSBindingCensusResult{}
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
