package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// InspectStructuralInventory returns bounded structural entity rows from the
// content index using only scoped, deterministic predicates.
func (cr *ContentReader) InspectStructuralInventory(
	ctx context.Context,
	req structuralInventoryRequest,
) ([]EntityContent, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "inspect_structural_inventory"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	where, args := structuralInventoryWhere(req)
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY repo_id, relative_path, start_line, entity_name, entity_id
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), limitArg, offsetArg)
	args = append(args, req.normalizedLimit(), req.Offset)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("inspect structural inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]EntityContent, 0, req.normalizedLimit())
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.SourceCache,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan structural inventory row: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan structural inventory row: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// CountStructuralInventoryByFile returns bounded function counts per file from
// the content index without hydrating source bodies.
func (cr *ContentReader) CountStructuralInventoryByFile(
	ctx context.Context,
	req structuralInventoryRequest,
) ([]StructuralInventoryFileCount, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "count_structural_inventory_by_file"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	countReq := req
	countReq.EntityKind = "function"
	where, args := structuralInventoryWhere(countReq)
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`
		SELECT repo_id, relative_path, coalesce(language, ''), count(*) AS function_count
		FROM content_entities
		WHERE %s
		GROUP BY repo_id, relative_path, coalesce(language, '')
		ORDER BY function_count DESC, repo_id, relative_path
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), limitArg, offsetArg)
	args = append(args, req.normalizedLimit(), req.Offset)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("count structural inventory by file: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]StructuralInventoryFileCount, 0, req.normalizedLimit())
	for rows.Next() {
		var row StructuralInventoryFileCount
		if err := rows.Scan(&row.RepoID, &row.RelativePath, &row.Language, &row.FunctionCount); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan structural inventory file count: %w", err)
		}
		row.SourceBackend = "postgres_content_store"
		row.MatchedKind = "function_count_by_file"
		row.SourceHandle = map[string]any{
			"repo_id":       row.RepoID,
			"file_path":     row.RelativePath,
			"relative_path": row.RelativePath,
			"content_tool":  "get_file_content",
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

func structuralInventoryWhere(req structuralInventoryRequest) ([]string, []any) {
	where := make([]string, 0, 10)
	args := make([]any, 0, 10)
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		where = append(where, "repo_id = "+addArg(repoID))
	}
	if entityType := req.entityType(); entityType != "" {
		where = append(where, "entity_type = "+addArg(entityType))
	}
	if filePath := strings.TrimSpace(req.FilePath); filePath != "" {
		where = append(where, "relative_path = "+addArg(filePath))
	}
	if symbol := strings.TrimSpace(req.Symbol); symbol != "" {
		where = append(where, "entity_name = "+addArg(symbol))
	}
	if language := strings.ToLower(strings.TrimSpace(req.Language)); language != "" {
		variants := normalizedLanguageVariants(language)
		parts := make([]string, 0, len(variants))
		for _, variant := range variants {
			parts = append(parts, "language = "+addArg(variant))
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	where = append(where, structuralInventoryKindPredicates(req, addArg)...)
	if len(where) == 0 {
		where = append(where, "true")
	}
	return where, args
}

func structuralInventoryKindPredicates(
	req structuralInventoryRequest,
	addArg func(any) string,
) []string {
	switch req.kind() {
	case "dataclass":
		return []string{"(metadata->'dead_code_root_kinds' ? 'python.dataclass_model' OR metadata->'decorators' ? '@dataclass' OR metadata->'decorators' ? '@dataclasses.dataclass')"}
	case "documented", "documented_function":
		return []string{"coalesce(metadata->>'docstring', '') <> ''"}
	case "decorated":
		predicates := []string{"jsonb_array_length(coalesce(metadata->'decorators', '[]'::jsonb)) > 0"}
		if decorator := strings.TrimSpace(req.Decorator); decorator != "" {
			raw := addArg(decorator)
			withAt := addArg(ensureAtDecorator(decorator))
			predicates = append(predicates, "((metadata->'decorators') ? "+raw+" OR (metadata->'decorators') ? "+withAt+")")
		}
		if className := strings.TrimSpace(req.ClassName); className != "" {
			predicates = append(predicates, classContextPredicate(addArg(className)))
		}
		return predicates
	case "class_with_method":
		return []string{"entity_name = " + addArg(strings.TrimSpace(req.MethodName)), "coalesce(metadata->>'class_context', metadata->>'context', metadata->>'impl_context', '') <> ''"}
	case "super_call":
		return []string{"(source_cache ILIKE '%super(%' OR source_cache ILIKE '%super.%' OR source_cache ILIKE '%super::%' OR source_cache ILIKE '%super %')"}
	case "top_level":
		return []string{"coalesce(metadata->>'class_context', metadata->>'context', metadata->>'impl_context', '') = ''"}
	default:
		return nil
	}
}

func classContextPredicate(arg string) string {
	return "(metadata->>'class_context' = " + arg +
		" OR metadata->>'context' = " + arg +
		" OR metadata->>'impl_context' = " + arg + ")"
}

func ensureAtDecorator(decorator string) string {
	decorator = strings.TrimSpace(decorator)
	if strings.HasPrefix(decorator, "@") {
		return decorator
	}
	return "@" + decorator
}
