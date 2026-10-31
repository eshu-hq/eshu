// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const maxRunningScannerFactPage = 1000

const runningScannerImageFactsPageQuery = `
SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS source_scope
  ON source_scope.scope_id = fact.scope_id
 AND source_scope.active_generation_id = fact.generation_id
JOIN collector_instances AS source_instance
  ON source_instance.instance_id = CASE
      WHEN fact.fact_kind = 'aws_resource'
      THEN fact.payload #>> '{attributes,collector_instance_id}'
      ELSE fact.payload->>'collector_instance_id'
  END
WHERE fact.fact_id > $1
  AND NOT fact.is_tombstone
  AND source_instance.enabled AND source_instance.claims_enabled
  AND (
      (fact.fact_kind = 'kubernetes_live.pod_template'
        AND source_instance.collector_kind = 'kubernetes_live')
      OR (fact.fact_kind = 'aws_image_reference'
        AND fact.payload->>'service_kind' = 'ecs'
        AND source_instance.collector_kind = 'aws')
      OR (fact.fact_kind = 'aws_resource'
        AND source_instance.collector_kind = 'aws'
        AND (
          (fact.payload->>'resource_type' = 'aws_ecs_task'
            AND fact.payload->>'state' = 'RUNNING')
          OR (fact.payload->>'resource_type' = 'aws_lambda_function'
            AND fact.payload #>> '{attributes,attributes,package_type}' = 'Image')
        ))
  )
ORDER BY fact.fact_id
LIMIT $2
`

// ListRunningScannerImageFactsPage reads one bounded page of currently active
// runtime evidence from enabled producers. The caller uses the last FactID as
// the next cursor and wraps after an empty page; it never infers a digest from
// a tag-only reference.
func (s FactStore) ListRunningScannerImageFactsPage(
	ctx context.Context,
	afterFactID string,
	limit int,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("running scanner image facts require database")
	}
	if limit <= 0 || limit > maxRunningScannerFactPage {
		return nil, fmt.Errorf("running scanner image fact page limit must be between 1 and %d", maxRunningScannerFactPage)
	}
	rows, err := s.database.QueryContext(ctx, runningScannerImageFactsPageQuery, afterFactID, limit)
	if err != nil {
		return nil, fmt.Errorf("read running scanner image facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]facts.Envelope, 0, limit)
	for rows.Next() {
		var fact facts.Envelope
		var raw []byte
		if err := rows.Scan(&fact.FactID, &fact.ScopeID, &fact.GenerationID,
			&fact.FactKind, &raw); err != nil {
			return nil, fmt.Errorf("read running scanner image facts: %w", err)
		}
		if err := json.Unmarshal(raw, &fact.Payload); err != nil {
			return nil, fmt.Errorf("decode running scanner image fact: %w", err)
		}
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read running scanner image facts: %w", err)
	}
	return out, nil
}
