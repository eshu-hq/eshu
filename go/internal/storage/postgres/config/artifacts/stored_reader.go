// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// storedReadSQL is the PR3c stored read (protocol section 4): one statement
// on the replica over the access-filtered set of related repository ids. It
// inner-joins state and versions, as the record proposes, and left-joins the
// rows, so a derived repository with no config artifacts still answers. One
// statement is one snapshot, which covers the trust comparison and the rows
// together. Every predicate is a primary-key equality on a table that grows
// with repositories, never a scan of a table that grows with files (S2 bar).
//
// Column order: state repo_id, content_version, extractor_version, row_count,
// current version, then the paths columns, then the state's list_truncated
// (the stored 5,000-window reason input, #8155 PR3c). The trust decision stays
// in Go: ReadStored classifies every repository with Classify, never a SQL
// copy of the rule. list_truncated rides on rows the join already fetches, so
// the predicates and the S2 bar (primary-key equality only, never a scan of a
// file-growth table) are unchanged.
const storedReadSQL = `
SELECT s.repo_id, s.content_version, s.extractor_version, s.row_count, v.version,
       p.raw_path, p.relative_path, p.evidence_kind, p.name_scoped, s.list_truncated
FROM config_artifact_repo_state AS s
JOIN config_artifact_content_versions AS v ON v.repo_id = s.repo_id
LEFT JOIN config_artifact_paths AS p ON p.repo_id = s.repo_id
WHERE s.repo_id = ANY($1)
ORDER BY s.repo_id`

// PostgresReader is the Postgres implementation of the read-model port. It
// holds the replica queryer and no metric, span or clock: the wiring slice
// counts the read, as the derive does.
type PostgresReader struct {
	queryer db.Queryer
}

// NewPostgresReader builds a stored reader over queryer, which is the
// replica in production. A nil queryer fails ReadStored, never a request.
func NewPostgresReader(queryer db.Queryer) PostgresReader {
	return PostgresReader{queryer: queryer}
}

// ReadStored answers with the trusted stored rows of the requested related
// repository ids, in request order, one Repository per distinct id. A
// repository the join drops (no state row, or a state row with no version
// row) answers ReasonMissing; any other untrusted repository answers the
// Classify reason with no rows. An empty id set sends no statement. An error
// fails the whole read; the caller then serves every repository live.
func (r PostgresReader) ReadStored(ctx context.Context, repoIDs []string, extractorVersion int) ([]Repository, error) {
	if r.queryer == nil {
		return nil, errors.New("read stored config artifacts: queryer is nil")
	}
	ids := dedupeRepoIDs(repoIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.queryer.QueryContext(ctx, storedReadSQL, array.StringArray(ids))
	if err != nil {
		return nil, fmt.Errorf("read stored config artifacts: %w", err)
	}
	scanned, err := scanStoredRows(rows)
	if err != nil {
		return nil, err
	}
	out := make([]Repository, 0, len(ids))
	for _, id := range ids {
		out = append(out, classifyStored(id, scanned[id], extractorVersion))
	}
	return out, nil
}

// storedState is one scanned state row with its current version.
type storedState struct {
	contentVersion   *int64
	extractorVersion int
	rowCount         *int
	version          int64
	listTruncated    bool
	rows             []Row
}

// scanStoredRows reads the stored read into one entry per returned
// repository. State columns repeat on every row of a repository; the first
// occurrence wins. A NULL paths group (the LEFT JOIN miss of a derived
// repository with no config artifacts) contributes no row.
func scanStoredRows(rows db.Rows) (map[string]*storedState, error) {
	defer func() { _ = rows.Close() }()
	scanned := map[string]*storedState{}
	for rows.Next() {
		var repoID sql.NullString
		var contentVersion, rowCount, version sql.NullInt64
		var extractorVersion sql.NullInt64
		var rawPath, relativePath, evidenceKind sql.NullString
		var nameScoped, listTruncated sql.NullBool
		if err := rows.Scan(&repoID, &contentVersion, &extractorVersion, &rowCount, &version,
			&rawPath, &relativePath, &evidenceKind, &nameScoped, &listTruncated); err != nil {
			return nil, fmt.Errorf("scan stored config artifacts: %w", err)
		}
		entry, ok := scanned[repoID.String]
		if !ok {
			entry = &storedState{
				extractorVersion: int(extractorVersion.Int64),
				version:          version.Int64,
				listTruncated:    listTruncated.Bool,
			}
			if contentVersion.Valid {
				entry.contentVersion = &contentVersion.Int64
			}
			if rowCount.Valid {
				count := int(rowCount.Int64)
				entry.rowCount = &count
			}
			scanned[repoID.String] = entry
		}
		if rawPath.Valid {
			entry.rows = append(entry.rows, Row{
				RawPath:      rawPath.String,
				RelativePath: relativePath.String,
				EvidenceKind: evidenceKind.String,
				NameScoped:   nameScoped.Bool,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stored config artifacts: %w", err)
	}
	return scanned, nil
}

// classifyStored applies the reader's trust condition to one repository: a
// repository the join dropped is missing, and any other repository is
// classified with Classify against the reader's extractor version. Only a
// trusted repository keeps its rows.
func classifyStored(repoID string, entry *storedState, extractorVersion int) Repository {
	if entry == nil {
		return Repository{RepoID: repoID, Reason: ReasonMissing}
	}
	state := &State{
		ContentVersion:   entry.contentVersion,
		ExtractorVersion: entry.extractorVersion,
		RowCount:         entry.rowCount,
	}
	version := entry.version
	reason := Classify(state, &version, extractorVersion)
	if reason != ReasonTrusted {
		return Repository{RepoID: repoID, Reason: reason}
	}
	return Repository{RepoID: repoID, Reason: ReasonTrusted, Rows: entry.rows, ListTruncated: entry.listTruncated}
}

// dedupeRepoIDs drops empty and repeated ids, keeping request order.
func dedupeRepoIDs(repoIDs []string) []string {
	seen := make(map[string]struct{}, len(repoIDs))
	ids := make([]string, 0, len(repoIDs))
	for _, id := range repoIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
