package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listFactsByKindQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = ANY($3::text[])
ORDER BY observed_at ASC, fact_id ASC
`

// ListFactsByKind loads fact envelopes for one scope generation and a bounded
// set of fact kinds, preserving the same stable ordering as ListFacts.
func (s FactStore) ListFactsByKind(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	factKinds = cleanFactKinds(factKinds)
	if len(factKinds) == 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, listFactsByKindQuery, scopeID, generationID, factKinds)
	if err != nil {
		return nil, fmt.Errorf("list facts by kind: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts by kind: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list facts by kind: %w", err)
	}

	return loaded, nil
}

// cleanFactKinds removes empty fact-kind filters while preserving first-seen
// order so tests and query plans stay stable.
func cleanFactKinds(factKinds []string) []string {
	cleaned := make([]string, 0, len(factKinds))
	seen := make(map[string]struct{}, len(factKinds))
	for _, factKind := range factKinds {
		factKind = strings.TrimSpace(factKind)
		if factKind == "" {
			continue
		}
		if _, ok := seen[factKind]; ok {
			continue
		}
		seen[factKind] = struct{}{}
		cleaned = append(cleaned, factKind)
	}
	return cleaned
}
