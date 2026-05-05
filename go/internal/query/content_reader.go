package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ContentReader reads file and entity content from the Postgres content store.
type ContentReader struct {
	db     *sql.DB
	tracer trace.Tracer
}

// NewContentReader constructs a Postgres-backed content store reader.
func NewContentReader(db *sql.DB) *ContentReader {
	return &ContentReader{
		db:     db,
		tracer: otel.Tracer("eshu/go/internal/query"),
	}
}

// EntityContent is one parsed entity from the content store.
type EntityContent struct {
	EntityID     string         `json:"entity_id"`
	RepoID       string         `json:"repo_id"`
	RelativePath string         `json:"relative_path"`
	EntityType   string         `json:"entity_type"`
	EntityName   string         `json:"entity_name"`
	StartLine    int            `json:"start_line"`
	EndLine      int            `json:"end_line"`
	Language     string         `json:"language,omitempty"`
	SourceCache  string         `json:"source_cache,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// GetFileContent returns one file by repo_id and relative_path.
func (cr *ContentReader) GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_file_content"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	row := cr.db.QueryRowContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       content, content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path = $2
	`, repoID, relativePath)

	var f FileContent
	err := row.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
		&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get file content: %w", err)
	}
	return &f, nil
}

// GetFileLines returns a line range from one file.
func (cr *ContentReader) GetFileLines(ctx context.Context, repoID, relativePath string, startLine, endLine int) (*FileContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_file_lines"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	fc, err := cr.GetFileContent(ctx, repoID, relativePath)
	if err != nil || fc == nil {
		if err != nil {
			span.RecordError(err)
		}
		return fc, err
	}

	lines := strings.Split(fc.Content, "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine < 1 || endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > len(lines) {
		fc.Content = ""
		fc.LineCount = 0
		return fc, nil
	}

	selected := lines[startLine-1 : endLine]
	fc.Content = strings.Join(selected, "\n")
	fc.LineCount = len(selected)
	return fc, nil
}

// GetEntityContent returns one entity by entity_id.
func (cr *ContentReader) GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_entity_content"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	row := cr.db.QueryRowContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_id = $1
	`, entityID)

	var e EntityContent
	var rawMetadata []byte
	err := row.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
		&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get entity content: %w", err)
	}
	e.Metadata, err = decodeEntityMetadata(rawMetadata)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get entity content: %w", err)
	}
	return &e, nil
}

// SearchFileContent searches file content using trigram matching.
func (cr *ContentReader) SearchFileContent(ctx context.Context, repoID, pattern string, limit int) ([]FileContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_file_content"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND content ILIKE '%' || $2 || '%'
		ORDER BY relative_path
		LIMIT $3
	`
	rows, err := cr.db.QueryContext(ctx, query, repoID, pattern, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan file search result: %w", err)
		}
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntityContent searches entity source cache using trigram matching.
func (cr *ContentReader) SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entity_content"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1 AND source_cache ILIKE '%' || $2 || '%'
		ORDER BY relative_path, start_line
		LIMIT $3
	`
	rows, err := cr.db.QueryContext(ctx, query, repoID, pattern, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entity content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity search result: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity search result: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesByName returns entities whose materialized name matches the
// requested pattern, optionally restricted to one entity type.
func (cr *ContentReader) SearchEntitiesByName(
	ctx context.Context,
	repoID string,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_name"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1 AND entity_name ILIKE '%' || $2 || '%'
	`
	args := []any{repoID, name}
	nextArg := 3
	if entityType != "" {
		filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
		query += ` AND ` + filter
		args = append(args, filterArgs...)
		nextArg = next
	}
	query += fmt.Sprintf(`
		ORDER BY relative_path, start_line
		LIMIT $%d
	`, nextArg)
	args = append(args, limit)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by name: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesReferencingComponent returns content entities whose metadata
// records JSX usage of the requested component name.
func (cr *ContentReader) SearchEntitiesReferencingComponent(
	ctx context.Context,
	repoID string,
	componentName string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_referencing_component"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1
		  AND coalesce(metadata -> 'jsx_component_usage', '[]'::jsonb) ? $2
		ORDER BY relative_path, start_line, entity_name
		LIMIT $3
	`, repoID, componentName, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities referencing component: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
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
			return nil, fmt.Errorf("scan referencing component result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan referencing component result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// ListRepoFiles returns all indexed files for one repository.
func (cr *ContentReader) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_files"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1
		ORDER BY relative_path
		LIMIT $2
	`, repoID, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo file: %w", err)
		}
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// ListRepoEntities returns all indexed entities for one repository.
func (cr *ContentReader) ListRepoEntities(ctx context.Context, repoID string, limit int) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_entities"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1
		ORDER BY relative_path, start_line
		LIMIT $2
	`, repoID, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo entity: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo entity: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesByLanguageAndType returns materialized content entities for one
// repo/language/entity-type filter using entity names as the primary lookup.
func (cr *ContentReader) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_language_and_type"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	languageVariants := normalizedLanguageVariants(language)
	filters := make([]string, 0, 4)
	args := make([]any, 0, 4)
	nextArg := 1
	if entityType != "" {
		filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
		filters = append(filters, filter)
		args = append(args, filterArgs...)
		nextArg = next
	}

	if repoID != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, repoID)
		nextArg++
	}

	if len(languageVariants) > 0 {
		parts := make([]string, 0, len(languageVariants))
		for _, variant := range languageVariants {
			parts = append(parts, fmt.Sprintf("language = $%d", nextArg))
			args = append(args, variant)
			nextArg++
		}
		filters = append(filters, "("+strings.Join(parts, " OR ")+")")
	}

	if query != "" {
		filters = append(filters, fmt.Sprintf("entity_name ILIKE $%d", nextArg))
		args = append(args, "%"+query+"%")
		nextArg++
	}

	sqlQuery := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY relative_path, start_line, entity_name
		LIMIT $%d
	`, strings.Join(filters, " AND "), nextArg)
	args = append(args, limit)

	rows, err := cr.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by language and type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
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
			return nil, fmt.Errorf("scan language/type entity result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan language/type entity result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}

	return results, nil
}

func decodeEntityMetadata(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode entity metadata: %w", err)
	}
	if len(metadata) == 0 {
		return nil, nil
	}
	return metadata, nil
}

// ListFrameworkRoutes queries fact_records for files with framework_semantics
// route detection for a given repo_id.
func (cr *ContentReader) ListFrameworkRoutes(ctx context.Context, repoID string) ([]FrameworkRouteEvidence, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_framework_routes"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	if repoID == "" {
		return nil, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT
			payload->>'relative_path',
			payload->'parsed_file_data'->'framework_semantics'
		FROM fact_records
		WHERE fact_kind = 'file'
		  AND payload->>'repo_id' = $1
		  AND payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
		  AND jsonb_array_length(
			  COALESCE(payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
		  ) > 0
		ORDER BY payload->>'relative_path'
	`, repoID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list framework routes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FrameworkRouteEvidence
	for rows.Next() {
		var relativePath string
		var rawSemantics []byte
		if err := rows.Scan(&relativePath, &rawSemantics); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan framework route: %w", err)
		}
		routes := parseFrameworkSemantics(relativePath, rawSemantics)
		results = append(results, routes...)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("framework routes iteration: %w", err)
	}
	return results, nil
}

// parseFrameworkSemantics decodes one framework_semantics JSON blob into
// FrameworkRouteEvidence entries — one per detected framework.
func parseFrameworkSemantics(relativePath string, raw []byte) []FrameworkRouteEvidence {
	if len(raw) == 0 {
		return nil
	}
	var semantics map[string]any
	if err := json.Unmarshal(raw, &semantics); err != nil {
		return nil
	}

	frameworksRaw, _ := semantics["frameworks"].([]any)
	if len(frameworksRaw) == 0 {
		return nil
	}

	results := make([]FrameworkRouteEvidence, 0, len(frameworksRaw))
	for _, fwRaw := range frameworksRaw {
		fw, _ := fwRaw.(string)
		if fw == "" {
			continue
		}
		fwData, _ := semantics[fw].(map[string]any)
		if fwData == nil {
			continue
		}
		if fw == "nextjs" {
			if route, ok := nextJSFrameworkRoute(relativePath, fwData); ok {
				results = append(results, route)
			}
			continue
		}
		routePaths := anySliceToStrings(fwData["route_paths"])
		if len(routePaths) == 0 {
			continue
		}
		results = append(results, FrameworkRouteEvidence{
			Framework:    fw,
			RelativePath: relativePath,
			RoutePaths:   routePaths,
			RouteMethods: anySliceToStrings(fwData["route_methods"]),
			RouteEntries: frameworkRouteEntries(fwData["route_entries"]),
		})
	}
	return results
}

// nextJSFrameworkRoute translates parser-owned Next.js route module metadata
// into the generic framework route shape used by service evidence.
func nextJSFrameworkRoute(relativePath string, frameworkData map[string]any) (FrameworkRouteEvidence, bool) {
	if stringValue(frameworkData["module_kind"]) != "route" {
		return FrameworkRouteEvidence{}, false
	}
	routePath := nextJSRoutePath(anySliceToStrings(frameworkData["route_segments"]))
	if routePath == "" {
		return FrameworkRouteEvidence{}, false
	}
	return FrameworkRouteEvidence{
		Framework:    "nextjs",
		RelativePath: relativePath,
		RoutePaths:   []string{routePath},
		RouteMethods: anySliceToStrings(frameworkData["route_verbs"]),
	}, true
}

// nextJSRoutePath preserves parser-emitted route segment names while removing
// Next.js route-group folders that are not part of the public URL.
func nextJSRoutePath(segments []string) string {
	pathSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || (strings.HasPrefix(segment, "(") && strings.HasSuffix(segment, ")")) {
			continue
		}
		pathSegments = append(pathSegments, segment)
	}
	if len(pathSegments) == 0 {
		return ""
	}
	return "/" + strings.Join(pathSegments, "/")
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return strings.TrimSpace(value)
}

func anySliceToStrings(raw any) []string {
	slice, _ := raw.([]any)
	if len(slice) == 0 {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

// frameworkRouteEntries decodes the parser's paired route evidence while
// tolerating older facts that only carry route_paths and route_methods.
func frameworkRouteEntries(raw any) []FrameworkRouteEntryEvidence {
	slice, _ := raw.([]any)
	if len(slice) == 0 {
		return nil
	}
	result := make([]FrameworkRouteEntryEvidence, 0, len(slice))
	for _, item := range slice {
		row, _ := item.(map[string]any)
		if len(row) == 0 {
			continue
		}
		entry := FrameworkRouteEntryEvidence{
			Method: strings.TrimSpace(stringValue(row["method"])),
			Path:   strings.TrimSpace(stringValue(row["path"])),
		}
		if entry.Method == "" || entry.Path == "" {
			continue
		}
		result = append(result, entry)
	}
	return result
}
