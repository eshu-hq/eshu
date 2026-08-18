// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var maxActiveCICDWorkflowImageFacts = 12_000

// listActiveCICDWorkflowImageFactsQuery uses the Git ingestion scope's
// partition_key as the indexed repository owner boundary. This avoids a
// platform-wide payload JSON scan while preserving both default-branch
// repository scopes and explicitly selected repository-ref scopes.
const listActiveCICDWorkflowImageFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM ingestion_scopes AS scope
JOIN fact_records AS fact
  ON fact.scope_id = scope.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE scope.partition_key = ANY($1::text[])
  AND scope.scope_kind IN ('repository', 'repository_ref')
  AND scope.source_system = 'git'
  AND scope.collector_kind = 'git'
  AND scope.status = 'active'
  AND generation.status = 'active'
  AND fact.fact_kind = 'ci.workflow_image_evidence'
  AND fact.source_system = 'git'
  AND fact.collector_kind = 'git'
  AND fact.is_tombstone = FALSE
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $2
`

// ListActiveCICDWorkflowImageFacts loads active Git workflow-image evidence
// for the requested CI run repository owners. A blank owner set returns
// without querying, and a result above the safety cap fails closed rather than
// silently truncating correlation evidence.
func (s FactStore) ListActiveCICDWorkflowImageFacts(
	ctx context.Context,
	repositoryIDs []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	repositoryIDs = cleanStringFilterValues(repositoryIDs)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActiveCICDWorkflowImageFactsQuery,
		pgarray.Array(repositoryIDs),
		maxActiveCICDWorkflowImageFacts+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list active CI/CD workflow image facts: %w", err)
	}
	loaded, err := scanCICDRunHistoryRows(rows, "list active CI/CD workflow image facts")
	if err != nil {
		return nil, err
	}
	if len(loaded) > maxActiveCICDWorkflowImageFacts {
		return nil, fmt.Errorf(
			"list active CI/CD workflow image facts: result exceeds safety cap %d for %d repositories",
			maxActiveCICDWorkflowImageFacts,
			len(repositoryIDs),
		)
	}
	return loaded, nil
}
