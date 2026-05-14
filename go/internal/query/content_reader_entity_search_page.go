package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// searchEntityContentPage searches one repository's cached entity snippets.
func (cr *ContentReader) searchEntityContentPage(
	ctx context.Context,
	repoID string,
	pattern string,
	limit int,
	offset int,
) ([]EntityContent, error) {
	return cr.searchEntityContentScoped(ctx, "search_entity_content_page", "repo_id = $1 AND source_cache ILIKE '%' || $2 || '%'", []any{repoID, pattern}, limit, offset)
}

// searchEntityContentInRepos searches cached snippets for an explicit repository set.
func (cr *ContentReader) searchEntityContentInRepos(
	ctx context.Context,
	repoIDs []string,
	pattern string,
	limit int,
	offset int,
) ([]EntityContent, error) {
	return cr.searchEntityContentScoped(ctx, "search_entity_content_in_repos", "repo_id = ANY(string_to_array($1, E'\\x1f')) AND source_cache ILIKE '%' || $2 || '%'", []any{strings.Join(repoIDs, "\x1f"), pattern}, limit, offset)
}

// searchEntityContentAnyRepoPage searches cached snippets across all repositories.
func (cr *ContentReader) searchEntityContentAnyRepoPage(
	ctx context.Context,
	pattern string,
	limit int,
	offset int,
) ([]EntityContent, error) {
	return cr.searchEntityContentScoped(ctx, "search_entity_content_any_repo_page", "source_cache ILIKE '%' || $1 || '%'", []any{pattern}, limit, offset)
}

// searchEntityContentScoped executes a bounded entity-content query using a
// fixed WHERE fragment selected by the caller.
func (cr *ContentReader) searchEntityContentScoped(
	ctx context.Context,
	operation string,
	where string,
	args []any,
	limit int,
	offset int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY repo_id, relative_path, start_line
		LIMIT $%d OFFSET $%d
	`, where, limitArg, offsetArg)
	args = append(args, limit, offset)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search paged entity content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&entity.EntityID, &entity.RepoID, &entity.RelativePath, &entity.EntityType, &entity.EntityName, &entity.StartLine, &entity.EndLine, &entity.Language, &entity.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan paged entity search result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan paged entity search result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
