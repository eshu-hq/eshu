package postgres

import (
	"context"
	"strings"
)

// lastProjectedCommitSHAQuery returns the source commit of the most recently
// ingested generation for a scope that reached a projected state. A generation
// is "projected" once it has been active, which is true for active, completed,
// and superseded statuses; pending and failed generations never projected and
// must be excluded, otherwise the baseline would advance past changes that were
// never materialized into the graph. The scope_id-leading index
// scope_generations_scope_idx (scope_id, status, ingested_at DESC) tightly
// bounds the scan; the small per-scope sort across the three projected statuses
// is capped by LIMIT 1, so the read stays cheap.
const lastProjectedCommitSHAQuery = `
SELECT source_commit_sha
FROM scope_generations
WHERE scope_id = $1
  AND status IN ('active', 'completed', 'superseded')
  AND source_commit_sha IS NOT NULL
  AND source_commit_sha <> ''
ORDER BY ingested_at DESC, generation_id DESC
LIMIT 1
`

// LastProjectedCommitSHA returns the source commit SHA of the most recent
// generation for scopeID that reached a projected state, or an empty string
// when the scope has no projected generation yet (first sync). It is the
// durable delta-sync baseline: diffing the next snapshot against this commit
// rather than the local working-copy HEAD prevents a projection that failed
// after a checkout advanced HEAD from silently skipping its changes.
//
// A blank scopeID returns an empty SHA without querying so callers can probe
// optimistically. Reachability of the returned SHA in the local checkout is
// the caller's concern: a SHA pruned by a shallow fetch is unreachable and the
// caller must fall back to a full snapshot rather than a broken delta.
func (s IngestionStore) LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error) {
	if s.db == nil || strings.TrimSpace(scopeID) == "" {
		return "", nil
	}

	rows, err := s.db.QueryContext(ctx, lastProjectedCommitSHAQuery, scopeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", rows.Err()
	}

	var sha string
	if err := rows.Scan(&sha); err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}
