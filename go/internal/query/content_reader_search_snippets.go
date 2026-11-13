// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// snippetReReadTimeout bounds the shape-B keyed snippet re-read (05a
// amendment): one second is ~100x the shim's 8.99 ms p95 at limit 200, so a
// trip means genuinely pathological rows, and the page still serves without
// snippets instead of eating the request budget.
const snippetReReadTimeout = time.Second

// searchFileContentScoped executes a bounded file-content query using a fixed
// WHERE fragment selected by the caller.
func (cr *ContentReader) searchFileContentScoped(
	ctx context.Context,
	operation string,
	pattern string,
	where string,
	args []any,
	limit int,
	offset int,
) (results []FileContent, err error) {
	start := time.Now()
	patternClass := scopedSearchPatternClass(pattern)
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()
	defer func() {
		recordScopedSearch(ctx, cr.instruments, span, operation, scopedSearchScopeClass(operation), patternClass, start, len(results), err, limit, offset)
	}()

	rawPatternArg := len(args) + 1
	limitArg := len(args) + 2
	offsetArg := len(args) + 3
	// Assembled from fileContentScopedQuery so the serving statement cannot
	// drift from the builder the derivation guard pins. The raw pattern
	// rides its own bind (position is literal); the WHERE fragment binds the
	// escaped LIKE literal (#10604).
	query := fileContentScopedQuery(where, rawPatternArg, limitArg, offsetArg)
	args = append(args, pattern, limit, offset)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("search paged file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var file FileContent
		var snippet sql.NullString
		var line sql.NullInt64
		if err := rows.Scan(&file.RepoID, &file.RelativePath, &file.CommitSHA, &file.Content,
			&file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType,
			&snippet, &line); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan paged file search result: %w", err)
		}
		// NULLs stay zero-valued (omitted on the wire): exact or absent,
		// never approximate.
		if snippet.Valid {
			file.MatchSnippet = snippet.String
		}
		if line.Valid {
			file.MatchLine = int(line.Int64)
		}
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// snippetReReadQuery builds the shape-B enrichment statement for n hit keys:
// the same snippet/line derivation as the scoped path, keyed by the exact
// (repo_id, relative_path, content_hash) triple. A row purged or rewritten
// between the walk and the re-read matches nothing and keeps its zero
// fields. patternArg is the 1-based position of the single raw-pattern bind
// after the 3n key binds.
func snippetReReadQuery(n int, patternArg int) string {
	tuples := make([]string, 0, n)
	for i := 0; i < n; i++ {
		base := i*3 + 1
		tuples = append(tuples, fmt.Sprintf("($%d,$%d,$%d)", base, base+1, base+2))
	}
	// #nosec G201 -- the tuple list is positional placeholders and patternArg is an integer; the pattern itself stays a bind
	return fmt.Sprintf(`
		SELECT repo_id, relative_path,
		       CASE WHEN spos.pos = 0 THEN NULL
		            ELSE substring(content FROM greatest(spos.pos - 40, 1) FOR 120) END AS match_snippet,
		       CASE WHEN spos.pos = 0 OR spos.pos - 1 > %d THEN NULL
		            ELSE (1 + (length(shead.head) - length(replace(shead.head, chr(10), '')))) END AS match_line
		FROM content_files,
		     LATERAL (SELECT position(lower($%d) IN content_lower) AS pos) AS spos,
		     LATERAL (SELECT substring(content_lower FROM 1 FOR greatest(spos.pos - 1, 0)) AS head) AS shead
		WHERE (repo_id, relative_path, content_hash) IN (%s)
	`, snippetMaxMatchLinePrefix, patternArg, strings.Join(tuples, ","))
}

// enrichFileSnippets fills match snippets and lines for unscoped walk hits
// in place (shape B, 05a amendment): one bounded keyed re-read under its own
// statement timeout. Best-effort by contract: a timeout, a stale key, or any
// re-read error leaves the row in place with both fields omitted, never
// fails the search, and never reorders the page.
func (cr *ContentReader) enrichFileSnippets(ctx context.Context, pattern string, files []querycontract.FileContent) {
	cr.enrichFileSnippetsWithTimeout(ctx, pattern, files, snippetReReadTimeout)
}

// enrichFileSnippetsWithTimeout is the timeout-injectable seam of
// enrichFileSnippets; production always passes snippetReReadTimeout.
func (cr *ContentReader) enrichFileSnippetsWithTimeout(ctx context.Context, pattern string, files []querycontract.FileContent, timeout time.Duration) {
	if len(files) == 0 {
		return
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_file_content_snippet_enrich"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	enriched, timedOut, runErr := cr.runSnippetReRead(ctx, pattern, files, timeout)
	// The pattern never enters a span attribute, label, or log line: only
	// counts, the timeout classification, and (for non-timeout failures) the
	// error itself.
	span.SetAttributes(
		attribute.Int("search.snippet_rows", len(files)),
		attribute.Int("search.snippet_enriched", enriched),
		attribute.Bool("search.snippet_timeout", timedOut),
	)
	if runErr != nil && !timedOut {
		span.RecordError(runErr)
		// Operator-visible signal: the span alone does not page anyone at
		// 3 AM, and omission here is silent by contract. The pattern never
		// enters the log line, only counts and the error itself.
		logger := cr.unscoped.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "search snippet enrichment degraded to omission",
			slog.Int("snippet_rows", len(files)),
			slog.Int("snippet_enriched", enriched),
			slog.String("error", runErr.Error()),
		)
	}
}

// runSnippetReRead executes one transaction-local keyed re-read and applies
// the hits to files. It reports how many rows it enriched, whether the
// re-read ended on a statement timeout, and any non-timeout error for the
// caller to record on the span. Every error path keeps the input rows
// untouched: degradation is omission, never a failed search.
func (cr *ContentReader) runSnippetReRead(ctx context.Context, pattern string, files []querycontract.FileContent, timeout time.Duration) (enriched int, timedOut bool, runErr error) {
	tx, err := cr.db.BeginReadOnlySnapshot(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Transaction-local, mirroring the unscoped walk's setTimeoutSQL: the
	// ROLLBACK above reverts it, so the session timeout never leaks.
	var applied string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('statement_timeout', $1, true)`,
		fmt.Sprintf("%dms", max(timeout, time.Millisecond).Milliseconds())).Scan(&applied); err != nil {
		return 0, false, err
	}

	args := make([]any, 0, len(files)*3+1)
	for _, file := range files {
		args = append(args, file.RepoID, file.RelativePath, file.ContentHash)
	}
	args = append(args, pattern)
	query := snippetReReadQuery(len(files), len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, isStatementTimeout(err), err
	}
	defer func() { _ = rows.Close() }()

	byKey := make(map[string]int, len(files))
	for i := range files {
		byKey[files[i].RepoID+"\x1f"+files[i].RelativePath] = i
	}
	for rows.Next() {
		var repoID, relativePath string
		var snippet sql.NullString
		var line sql.NullInt64
		if err := rows.Scan(&repoID, &relativePath, &snippet, &line); err != nil {
			return enriched, false, err
		}
		index, ok := byKey[repoID+"\x1f"+relativePath]
		if !ok {
			continue
		}
		if snippet.Valid {
			files[index].MatchSnippet = snippet.String
			enriched++
		}
		if line.Valid {
			files[index].MatchLine = int(line.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return enriched, isStatementTimeout(err), err
	}
	return enriched, false, nil
}

// isStatementTimeout reports whether err is a server-side statement
// cancellation (SQLSTATE 57014), the same classification the unscoped walk
// uses for its budget accounting.
func isStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}
