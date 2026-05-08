package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DeadCodeIncomingEntityIDs returns candidate entity IDs that have completed
// reducer code-call or metaclass incoming edges in the relational read model.
func (cr *ContentReader) DeadCodeIncomingEntityIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]bool, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]bool{}, nil
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "dead_code_incoming_entity_ids"),
			attribute.String("db.sql.table", "shared_projection_intents"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	args := make([]any, 0, len(entityIDs)+1)
	args = append(args, repoID)
	for i, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+2))
	}
	entityIDSet := strings.Join(placeholders, ", ")
	query := `
		SELECT DISTINCT incoming_entity_id
		FROM (
			SELECT payload->>'callee_entity_id' AS incoming_entity_id
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND payload->>'callee_entity_id' IN (` + entityIDSet + `)
			UNION
			SELECT payload->>'target_entity_id' AS incoming_entity_id
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND payload->>'relationship_type' = 'USES_METACLASS'
			  AND payload->>'target_entity_id' IN (` + entityIDSet + `)
		) incoming
		WHERE incoming_entity_id IS NOT NULL
		  AND incoming_entity_id <> ''
	`
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("dead code incoming entity ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incoming := make(map[string]bool)
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan dead code incoming entity id: %w", err)
		}
		incoming[entityID] = true
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return incoming, nil
}

func cleanDeadCodeIncomingEntityIDs(entityIDs []string) []string {
	if len(entityIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}
