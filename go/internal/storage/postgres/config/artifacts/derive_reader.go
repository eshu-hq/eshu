// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// contentReader is step 2's content_files reader. It sends the statements and
// row mappers of the neutral package (ListStatement, GetFilesStatement,
// NonBlankPathsStatement, FileScanTargets), the same ones the live query
// reader sends, so a stored answer cannot drift from the live one in ORDER
// BY, the artifact_type mapping or the blank predicate. It runs on the pool:
// the neutral package issues up to ReadChunksInFlight reads at once.
type contentReader struct {
	queryer db.Queryer
}

// ListFiles lists up to limit file rows of a repository with empty content.
func (r contentReader) ListFiles(ctx context.Context, repoID string, limit int) ([]contentartifacts.File, error) {
	rows, err := r.queryer.QueryContext(ctx, contentartifacts.ListStatement, repoID, limit)
	if err != nil {
		return nil, fmt.Errorf("list content files: %w", err)
	}
	return scanFiles(rows)
}

// ReadFiles reads the stored rows of relativePaths in one statement. The input
// is deduplicated on exact value, as the live reader does; an empty input
// sends no statement.
func (r contentReader) ReadFiles(ctx context.Context, repoID string, relativePaths []string) ([]contentartifacts.File, error) {
	if len(relativePaths) == 0 {
		return nil, nil
	}
	paths := make(array.StringArray, 0, len(relativePaths))
	seen := make(map[string]struct{}, len(relativePaths))
	for _, relativePath := range relativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	rows, err := r.queryer.QueryContext(ctx, contentartifacts.GetFilesStatement, repoID, paths)
	if err != nil {
		return nil, fmt.Errorf("read content files: %w", err)
	}
	return scanFiles(rows)
}

// NonBlankPaths returns the paths whose stored content is not blank under
// strings.TrimSpace, without moving content bytes.
func (r contentReader) NonBlankPaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error) {
	if len(relativePaths) == 0 {
		return nil, nil
	}
	rows, err := r.queryer.QueryContext(ctx, contentartifacts.NonBlankPathsStatement,
		repoID, array.StringArray(relativePaths), contentartifacts.SpaceCharacters())
	if err != nil {
		return nil, fmt.Errorf("non-blank content file paths: %w", err)
	}
	defer func() { _ = rows.Close() }()
	nonBlank := make([]string, 0, len(relativePaths))
	for rows.Next() {
		var relativePath string
		if err := rows.Scan(contentartifacts.RelativePathScanTargets(&relativePath)...); err != nil {
			return nil, fmt.Errorf("scan non-blank content file path: %w", err)
		}
		nonBlank = append(nonBlank, relativePath)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("non-blank content file paths: %w", err)
	}
	return nonBlank, nil
}

// scanFiles maps rows of the neutral file statements. An error returns no
// rows.
func scanFiles(rows db.Rows) ([]contentartifacts.File, error) {
	defer func() { _ = rows.Close() }()
	var files []contentartifacts.File
	for rows.Next() {
		var file contentartifacts.File
		if err := rows.Scan(contentartifacts.FileScanTargets(&file)...); err != nil {
			return nil, fmt.Errorf("scan content file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read content files: %w", err)
	}
	return files, nil
}

// storedRows converts the name-free build of step 2 to stored rows. Each
// row's NameScoped comes from contentartifacts.NameScoped, the one name-scope
// table. A row of an evidence kind no extractor emits, or a row that carries a
// repository name, fails the derive: the read step could not apply the name
// to it correctly.
func storedRows(built []map[string]any) ([]Row, error) {
	rows := make([]Row, 0, len(built))
	for _, item := range built {
		kind := rowString(item, "evidence_kind")
		scoped, known := contentartifacts.NameScoped(kind)
		if !known {
			return nil, fmt.Errorf("config artifact row has evidence kind %q, which no extractor emits", kind)
		}
		if name := rowString(item, "source_repo"); name != "" {
			return nil, fmt.Errorf("config artifact row carries source_repo %q; the stored build is name-free", name)
		}
		rows = append(rows, Row{
			RawPath:      rowString(item, "path"),
			RelativePath: rowString(item, "relative_path"),
			EvidenceKind: kind,
			NameScoped:   scoped,
		})
	}
	return rows, nil
}

// rowString reads a column of a built row the way the neutral package's row
// order and dedup read it: a missing or nil column is empty, a non-string
// value is formatted with %v.
func rowString(row map[string]any, key string) string {
	switch value := row[key].(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprintf("%v", value)
	}
}
