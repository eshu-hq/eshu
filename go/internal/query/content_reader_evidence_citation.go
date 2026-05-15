package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// evidenceCitationFiles hydrates bounded file handles in one content-store read.
func (cr *ContentReader) evidenceCitationFiles(
	ctx context.Context,
	lookups []evidenceCitationFileLookup,
) (map[evidenceCitationFileKey]FileContent, error) {
	lookups = cleanEvidenceCitationFileLookups(lookups)
	if cr == nil || cr.db == nil || len(lookups) == 0 {
		return map[evidenceCitationFileKey]FileContent{}, nil
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "evidence_citation_files"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	values := make([]string, 0, len(lookups))
	args := make([]any, 0, len(lookups)*3)
	for i, lookup := range lookups {
		base := i*3 + 1
		values = append(values, fmt.Sprintf("($%d, $%d, $%d)", base, base+1, base+2))
		args = append(args, lookup.RepoID, lookup.RelativePath, i)
	}
	rows, err := cr.db.QueryContext(ctx, `
		WITH requested(repo_id, relative_path, ordinal) AS (
			VALUES `+strings.Join(values, ", ")+`
		)
		SELECT cf.repo_id, cf.relative_path, coalesce(cf.commit_sha, ''),
		       cf.content, cf.content_hash, cf.line_count, coalesce(cf.language, ''),
		       coalesce(cf.artifact_type, '')
		FROM requested r
		JOIN content_files cf
		  ON cf.repo_id = r.repo_id
		 AND cf.relative_path = r.relative_path
		ORDER BY r.ordinal
	`, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get evidence citation files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	files := make(map[evidenceCitationFileKey]FileContent, len(lookups))
	for rows.Next() {
		var file FileContent
		if err := rows.Scan(
			&file.RepoID,
			&file.RelativePath,
			&file.CommitSHA,
			&file.Content,
			&file.ContentHash,
			&file.LineCount,
			&file.Language,
			&file.ArtifactType,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan evidence citation file: %w", err)
		}
		files[evidenceCitationFileKey{repoID: file.RepoID, relativePath: file.RelativePath}] = file
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return files, nil
}

func cleanEvidenceCitationFileLookups(lookups []evidenceCitationFileLookup) []evidenceCitationFileLookup {
	if len(lookups) == 0 {
		return nil
	}
	seen := make(map[evidenceCitationFileKey]struct{}, len(lookups))
	cleaned := make([]evidenceCitationFileLookup, 0, len(lookups))
	for _, lookup := range lookups {
		lookup.RepoID = strings.TrimSpace(lookup.RepoID)
		lookup.RelativePath = strings.TrimSpace(filepathSlash(lookup.RelativePath))
		if lookup.RepoID == "" || lookup.RelativePath == "" {
			continue
		}
		key := evidenceCitationFileKey{repoID: lookup.RepoID, relativePath: lookup.RelativePath}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, lookup)
	}
	return cleaned
}

func filepathSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
