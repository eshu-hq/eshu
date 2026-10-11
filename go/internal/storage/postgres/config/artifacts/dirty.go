// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// dirtySQL is the walker's dirty join (protocol section 1): every repository
// in the versions table whose stored rows need a derive. It is wider than the
// reader's trust condition: a repository whose last derive failed after an
// earlier success (retry) is work even when its rows still match the current
// version.
//
// $1 is the walker's extractor version, $2 the failure count that skips a
// repository, $3 the row limit. A repository is skipped only while its last
// failure came from the current extractor version, so a new extractor retries
// it; a successful derive resets failure_count. The walker leaves every state
// row from a newer extractor to the newer replicas. For a row with a
// successful derive, protocol section 3 step 3 forbids this binary's derive
// from replacing it, so listing it would only make an older replica derive
// and discard it every cycle. A failure-only row (row_count NULL) could be
// replaced under step 3's first, Proposed, clause; leaving it is a design
// choice the record leaves open: a newer replica or the next content Write
// derives it, and readers serve it live as failed meanwhile.
//
// The join reads two tables that grow with repositories, not with files. With
// a generic plan it is a hash join over two sequential scans; the measured
// figures are in the package README.
const dirtySQL = `
SELECT repo_id, reason, skipped
FROM (
    SELECT v.repo_id,
           CASE
               WHEN s.repo_id IS NULL THEN 'missing'
               WHEN s.row_count IS NULL THEN 'failed'
               WHEN s.failure_count > 0 THEN 'retry'
               WHEN s.extractor_version < $1 THEN 'stale_extractor'
               ELSE 'stale_version'
           END AS reason,
           coalesce(s.failure_count >= $2 AND s.last_failure_extractor_version = $1, false) AS skipped
    FROM config_artifact_content_versions AS v
    LEFT JOIN config_artifact_repo_state AS s ON s.repo_id = v.repo_id
    WHERE (s.repo_id IS NULL OR s.extractor_version <= $1)
      AND (s.repo_id IS NULL
           OR s.row_count IS NULL
           OR s.failure_count > 0
           OR s.extractor_version < $1
           OR s.content_version IS DISTINCT FROM v.version)
) AS dirty
ORDER BY skipped,
         CASE reason
             WHEN 'failed' THEN 0
             WHEN 'retry' THEN 1
             WHEN 'stale_extractor' THEN 2
             WHEN 'missing' THEN 3
             ELSE 4
         END,
         repo_id
LIMIT $3`

// DirtyRequest selects the walker's work.
type DirtyRequest struct {
	// ExtractorVersion is the walker binary's extractor version.
	ExtractorVersion int
	// SkipAfterFailures is the failure count after which a repository is
	// skipped for the current extractor version (5 in the record).
	SkipAfterFailures int
	// Limit bounds the rows returned. It must be positive.
	Limit int
}

// Dirty is one repository the walker must derive, or reports as skipped.
// Reason is ReasonFailed (no successful derive yet), ReasonRetry (a failed
// derive after a success), ReasonStaleExtractor, ReasonMissing (no state
// row), or ReasonStaleVersion. Skipped repositories are listed for status and
// not derived.
type Dirty struct {
	RepoID  string
	Reason  Reason
	Skipped bool
}

// ListDirty returns up to req.Limit repositories from the dirty join, work
// first in the order failed, retry, stale_extractor, missing, stale_version
// (then repo_id), and skipped repositories last.
func ListDirty(ctx context.Context, queryer db.Queryer, req DirtyRequest) ([]Dirty, error) {
	if req.Limit <= 0 {
		return nil, fmt.Errorf("list dirty config artifact repositories: limit must be positive, got %d", req.Limit)
	}
	rows, err := queryer.QueryContext(ctx, dirtySQL, req.ExtractorVersion, req.SkipAfterFailures, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("list dirty config artifact repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Dirty
	for rows.Next() {
		var item Dirty
		var reason string
		if err := rows.Scan(&item.RepoID, &reason, &item.Skipped); err != nil {
			return nil, fmt.Errorf("scan dirty config artifact repository: %w", err)
		}
		item.Reason = Reason(reason)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list dirty config artifact repositories: %w", err)
	}
	return out, nil
}
