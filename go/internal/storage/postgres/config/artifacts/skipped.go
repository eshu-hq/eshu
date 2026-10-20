// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// skippedSQL reports which of the given repositories the walker must not
// derive: failure_count at or over the limit with the last failure from the
// given extractor version. The dirty join reports the same predicate per
// listed repository; this statement answers it for walk-claimed ids the
// dirty list may not name (it is bounded while the walk is not). A
// repository whose skip exited — a content Write derived it, or the
// extractor moved — reads a lower count or an older failure version and is
// not reported.
const skippedSQL = `
SELECT repo_id FROM config_artifact_repo_state
WHERE repo_id = ANY($1::text[])
  AND failure_count >= $2
  AND last_failure_extractor_version = $3`

// SkippedRequest selects the skip check.
type SkippedRequest struct {
	// ExtractorVersion is the walker binary's extractor version.
	ExtractorVersion int
	// SkipAfterFailures is the failure count after which a repository is
	// skipped for the current extractor version (5 in the record).
	SkipAfterFailures int
}

// SkippedRepos returns the subset of repoIDs the walker must not derive. An
// empty input asks nothing and returns nothing. Every query error is
// returned, including a missing table: the cycle lists the dirty join first,
// so by the time this runs the tables exist and a missing one is a broken
// install, not a state to report.
func SkippedRepos(
	ctx context.Context,
	queryer db.Queryer,
	req SkippedRequest,
	repoIDs []string,
) ([]string, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	rows, err := queryer.QueryContext(ctx, skippedSQL,
		array.StringArray(repoIDs), req.SkipAfterFailures, req.ExtractorVersion)
	if err != nil {
		return nil, fmt.Errorf("list skipped config artifact repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var repoID string
		if err := rows.Scan(&repoID); err != nil {
			return nil, fmt.Errorf("scan skipped config artifact repository: %w", err)
		}
		out = append(out, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list skipped config artifact repositories: %w", err)
	}
	return out, nil
}
