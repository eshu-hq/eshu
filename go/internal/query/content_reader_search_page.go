// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SearchFiles chooses the narrowest paged SQL shape for a file-content search.
// Exported (#6060) so the optional-interface type assertion keeps matching
// this method: Go qualifies an unexported method name by its declaring
// package for interface satisfaction, so an unexported searchFiles here
// would silently stop satisfying querycontract.PagedContentSearcher -- no
// compile error, just a silent fallback to the slower per-request search
// path. The parameters are decomposed primitives (rather than the handler's
// unexported request type) for the same reason: an unexported parameter type
// cannot be named from the contentread package that asserts the interface
// since the lane-B1 move, so sharing the request type would break
// satisfaction the same silent way.
func (cr *ContentReader) SearchFiles(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]FileContent, error) {
	if len(repoIDs) > 1 {
		return cr.searchFileContentInRepos(ctx, repoIDs, pattern, limit, offset)
	}
	if repoID != "" {
		return cr.searchFileContentPage(ctx, repoID, pattern, limit, offset)
	}
	return cr.searchFileContentAnyRepoPage(ctx, pattern, limit, offset)
}

// SearchEntities chooses the narrowest paged SQL shape for an entity-content
// search. Exported for the same #6060 interface-export reason as SearchFiles
// above.
func (cr *ContentReader) SearchEntities(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]EntityContent, error) {
	if len(repoIDs) > 1 {
		return cr.searchEntityContentInRepos(ctx, repoIDs, pattern, limit, offset)
	}
	if repoID != "" {
		return cr.searchEntityContentPage(ctx, repoID, pattern, limit, offset)
	}
	return cr.searchEntityContentAnyRepoPage(ctx, pattern, limit, offset)
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
	return cr.searchFileContentScoped(ctx, "search_file_content_any_repo_page", "eshu_require_content_substring_indexes_ready() AND content ILIKE '%' || $1 || '%'", []any{pattern}, limit, offset)
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
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	// #nosec G201 -- `where` is a compile-time literal string chosen by the caller (e.g. "repo_id = $1 AND content ILIKE '%' || $2 || '%'"); limitArg/offsetArg are integers; no user data concatenated into SQL
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
		err = contentSubstringIndexReadError(err)
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

func codeTopicFilters(req codequery.CodeTopicInvestigationRequest) ([]string, []any, int) {
	filters := make([]string, 0, 3)
	args := make([]any, 0, 3)
	nextArg := 1
	if strings.TrimSpace(req.RepoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.RepoID))
		nextArg++
	} else {
		filters = append(filters, "eshu_require_content_substring_indexes_ready()")
		// #5167 W3 P1: bind a corpus-wide search to the caller's grant so the
		// LIMIT/OFFSET page is taken from the granted set, not cross-tenant.
		filters, args, nextArg = appendRepositoryGrantFilter(filters, args, nextArg, req.AllowedRepositoryIDs)
	}
	if strings.TrimSpace(req.Language) != "" {
		filters = append(filters, fmt.Sprintf("coalesce(language, '') = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.Language))
		nextArg++
	}
	return filters, args, nextArg
}

func splitCodeTopicTerms(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, "\x1f")
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}

// appendRepositoryGrantFilter binds a corpus-wide content read to the caller's
// granted repository ids at the SQL WHERE (#5167 W3 P1 filter-before-limit).
// Shared by codeTopicFilters, symbolSearchFilters, hardcodedSecretFilters and
// structuralInventoryWhere. Empty is a no-op (unscoped shared/admin callers);
// a grantless SCOPED caller never reaches here (codeContentGrantScope closes
// it first). nextArg must equal len(args)+1; returns the next free index.
func appendRepositoryGrantFilter(filters []string, args []any, nextArg int, allowedRepositoryIDs []string) ([]string, []any, int) {
	if len(allowedRepositoryIDs) == 0 {
		return filters, args, nextArg
	}
	filters = append(filters, fmt.Sprintf("repo_id = ANY($%d)", nextArg))
	args = append(args, array.Of(allowedRepositoryIDs))
	return filters, args, nextArg + 1
}

// recordCodeTopicFallbackReason records on span why a 16-term code-topic read
// took the single-statement path instead of the four-partition shared-snapshot
// path. Only the 16-term shape can run in parallel, so any other term count
// records nothing. The reservation-timeout reason is recorded by the caller,
// which is the only place that sees the reservation error.
func recordCodeTopicFallbackReason(span trace.Span, termCount int, supportsSnapshotSet bool, maxOpenConns int) {
	if termCount != 16 {
		return
	}
	if !supportsSnapshotSet {
		span.SetAttributes(attribute.String("code_topic.parallel_fallback_reason", "snapshot_set_unavailable"))
	} else if maxOpenConns < codetopicparallel.Partitions {
		span.SetAttributes(attribute.String("code_topic.parallel_fallback_reason", "pool_capacity"))
	}
}

// codeTopicTermHasTrigram reports whether an ILIKE '%term%' pattern has a run
// of three ASCII letters or digits, the shortest word pg_trgm can reliably
// extract a trigram from. In a LIKE pattern "_" and "%" are wildcards and any
// other character that is not an ASCII letter or digit breaks a word, so "db_"
// and "a_b" carry no trigram: both trigram GIN scans then return every row.
// Only ASCII counts because, under a C-ctype database, PostgreSQL treats a
// multibyte character as non-alphanumeric for trigram extraction, so a purely
// non-ASCII term has none there. A term this reports false for (for example
// "ab-cd", which does have trigrams, or a non-ASCII word) keeps the plain
// statement, which is the base, so the guard is safe to over-reject. The scoped
// one-term statement hides the term from the planner (#7246) only when this
// holds (measured 3.3 to 4.6 s plain against 14.9 to 16.7 s hidden for a term
// with none, on ops-qa).
func codeTopicTermHasTrigram(term string) bool {
	run := 0
	for _, r := range term {
		if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			run++
			if run >= 3 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
