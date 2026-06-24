// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"time"
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

// lastFullProjectionAtQuery returns the ingest time of the most recent
// generation for a scope that was a FULL (non-delta) observation and reached a
// projected state. The reconciliation sweep uses this to find scopes overdue for
// a full re-observation: delta generations accumulate and a missed deletion or
// failed retraction can leave stale graph nodes, so a periodic full snapshot is
// re-projected to retract any drift the delta path could not.
const lastFullProjectionAtQuery = `
SELECT ingested_at
FROM scope_generations
WHERE scope_id = $1
  AND status IN ('active', 'completed', 'superseded')
  AND is_delta = false
ORDER BY ingested_at DESC, generation_id DESC
LIMIT 1
`

// LastFullProjectionAt returns the ingest time of the most recent full
// (non-delta) generation for scopeID that reached a projected state, and whether
// one exists. A scope with no full projection yet (false) is treated by the
// reconciliation policy as immediately due, so its first observation establishes
// the baseline. A blank scopeID returns (zero, false) without querying.
func (s IngestionStore) LastFullProjectionAt(ctx context.Context, scopeID string) (time.Time, bool, error) {
	if s.db == nil || strings.TrimSpace(scopeID) == "" {
		return time.Time{}, false, nil
	}

	rows, err := s.db.QueryContext(ctx, lastFullProjectionAtQuery, scopeID)
	if err != nil {
		return time.Time{}, false, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return time.Time{}, false, rows.Err()
	}

	var ingestedAt time.Time
	if err := rows.Scan(&ingestedAt); err != nil {
		return time.Time{}, false, err
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, err
	}
	return ingestedAt, true, nil
}
