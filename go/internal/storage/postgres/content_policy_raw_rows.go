// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// contentPolicyRawRowsQuery counts the unsuppressed hardcoded-secret lines on a
// governed path that still hold a raw copy: no REDACTED_<finding_kind> marker
// and not a value-free HCL block opener (the skeleton keeps app_secrets = {
// verbatim). A marker line keeps its finding by design, so a "body matches
// the detector" test would never read zero (#8813 cleanup runbook).
var contentPolicyRawRowsQuery = `
SELECT count(*)
FROM content_file_secret_lines
WHERE NOT suppressed
  AND relative_path ~* $1
  AND line_text !~ '` + detect.MarkerPattern + `'
  AND line_text !~* '` + detect.BlockOpenerPattern + `'
`

// ContentPolicyRawRowsStore counts the detector-visible raw credential lines
// still stored on paths a content-store policy governs: unsuppressed findings
// whose line carries no REDACTED_<finding_kind> marker and is not a value-free
// HCL block opener. It backs the eshu_dp_content_policy_raw_rows gauge (#8833):
// zero means no governed row keeps a detector-visible raw line. It does not
// prove every governed file was stored again under its policy — a
// marker-redacted row keeps its finding rows by design — so the cleanup
// runbook's census raw_files column is the re-storage proof.
type ContentPolicyRawRowsStore struct {
	db          db.Queryer
	pathPattern string
}

// NewContentPolicyRawRowsStore returns a store that counts raw rows on the
// paths pathPattern matches (parser.Registry.ContentStorePathPattern, applied
// case-insensitively). An empty pattern means no governed path, and the count
// is zero without a query.
func NewContentPolicyRawRowsStore(queryer db.Queryer, pathPattern string) ContentPolicyRawRowsStore {
	return ContentPolicyRawRowsStore{db: queryer, pathPattern: pathPattern}
}

// ContentPolicyRawRows returns the number of raw credential lines still stored
// on governed paths.
func (s ContentPolicyRawRowsStore) ContentPolicyRawRows(ctx context.Context) (int64, error) {
	if s.pathPattern == "" {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, contentPolicyRawRowsQuery, s.pathPattern)
	if err != nil {
		return 0, fmt.Errorf("count content policy raw rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var count int64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan content policy raw rows: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read content policy raw rows: %w", err)
	}
	return count, nil
}
