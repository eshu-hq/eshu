package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActiveCICDRunCorrelationFactsQuery = `
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
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'digest' = ANY($1::text[])
  AND ($2 = '' OR fact.fact_id > $2)
ORDER BY fact.fact_id ASC
LIMIT $3
`

// ListActiveCICDRunCorrelationFacts loads active reducer-owned container image
// identity rows for the artifact digests observed in one CI/CD run generation.
func (s FactStore) ListActiveCICDRunCorrelationFacts(
	ctx context.Context,
	digests []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	digests = cleanStringFilterValues(digests)
	if len(digests) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorFactID string
	for {
		page, err := s.listActiveCICDRunCorrelationFactsPage(ctx, digests, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}
		cursorFactID = page[len(page)-1].FactID
	}
}

func (s FactStore) listActiveCICDRunCorrelationFactsPage(
	ctx context.Context,
	digests []string,
	cursorFactID string,
) ([]facts.Envelope, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listActiveCICDRunCorrelationFactsQuery,
		digests,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active ci/cd run correlation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, len(digests))
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active ci/cd run correlation facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active ci/cd run correlation facts: %w", err)
	}
	return loaded, nil
}

func cleanStringFilterValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	slices.Sort(cleaned)
	return cleaned
}
