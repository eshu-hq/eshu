package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// pagedContentSearcher is implemented by content readers that can push offset
// and multi-repo scope into SQL instead of paginating in handler memory.
type pagedContentSearcher interface {
	searchFiles(context.Context, contentSearchRequest) ([]FileContent, error)
	searchEntities(context.Context, contentSearchRequest) ([]EntityContent, error)
}

// searchFiles chooses the narrowest paged SQL shape for a file-content search.
func (cr *ContentReader) searchFiles(ctx context.Context, req contentSearchRequest) ([]FileContent, error) {
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 1 {
		return cr.searchFileContentInRepos(ctx, repoIDs, req.pattern(), req.limit()+1, req.offset())
	}
	if repoID := req.repoID(); repoID != "" {
		return cr.searchFileContentPage(ctx, repoID, req.pattern(), req.limit()+1, req.offset())
	}
	return cr.searchFileContentAnyRepoPage(ctx, req.pattern(), req.limit()+1, req.offset())
}

// searchEntities chooses the narrowest paged SQL shape for an entity-content search.
func (cr *ContentReader) searchEntities(ctx context.Context, req contentSearchRequest) ([]EntityContent, error) {
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 1 {
		return cr.searchEntityContentInRepos(ctx, repoIDs, req.pattern(), req.limit()+1, req.offset())
	}
	if repoID := req.repoID(); repoID != "" {
		return cr.searchEntityContentPage(ctx, repoID, req.pattern(), req.limit()+1, req.offset())
	}
	return cr.searchEntityContentAnyRepoPage(ctx, req.pattern(), req.limit()+1, req.offset())
}

// searchFileContentPage searches one repository with deterministic pagination.
func (cr *ContentReader) searchFileContentPage(
	ctx context.Context,
	repoID string,
	pattern string,
	limit int,
	offset int,
) ([]FileContent, error) {
	return cr.searchFileContentScoped(ctx, "search_file_content_page", "repo_id = $1 AND content ILIKE '%' || $2 || '%'", []any{repoID, pattern}, limit, offset)
}

// searchFileContentInRepos searches an explicit repository set with one SQL query.
func (cr *ContentReader) searchFileContentInRepos(
	ctx context.Context,
	repoIDs []string,
	pattern string,
	limit int,
	offset int,
) ([]FileContent, error) {
	return cr.searchFileContentScoped(ctx, "search_file_content_in_repos", "repo_id = ANY(string_to_array($1, E'\\x1f')) AND content ILIKE '%' || $2 || '%'", []any{strings.Join(repoIDs, "\x1f"), pattern}, limit, offset)
}

// searchFileContentAnyRepoPage searches all indexed repositories with deterministic pagination.
func (cr *ContentReader) searchFileContentAnyRepoPage(
	ctx context.Context,
	pattern string,
	limit int,
	offset int,
) ([]FileContent, error) {
	return cr.searchFileContentScoped(ctx, "search_file_content_any_repo_page", "content ILIKE '%' || $1 || '%'", []any{pattern}, limit, offset)
}

// searchFileContentScoped executes a bounded file-content query using a fixed
// WHERE fragment selected by the caller.
func (cr *ContentReader) searchFileContentScoped(
	ctx context.Context,
	operation string,
	where string,
	args []any,
	limit int,
	offset int,
) ([]FileContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE %s
		ORDER BY repo_id, relative_path
		LIMIT $%d OFFSET $%d
	`, where, limitArg, offsetArg)
	args = append(args, limit, offset)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search paged file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var file FileContent
		if err := rows.Scan(&file.RepoID, &file.RelativePath, &file.CommitSHA, &file.Content, &file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan paged file search result: %w", err)
		}
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
