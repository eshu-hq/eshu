// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// catchUpQuery owes an obligation to each active generation, in one bounded
// keyset page of ingestion_scopes, that has neither an obligation nor its own
// backward-evidence phase yet and that carries a repository fact (only those
// generations can ever publish the phase). It covers generations activated
// before the Ack insert shipped, or while the insert's migration was absent.
// The page is bounded by scope count, not by matches, so a pass never scans
// the whole scope table. The returned cursor is the last scope id the page
// read; an empty cursor means the page reached the end.
const catchUpQuery = `
WITH page AS (
    SELECT scope.scope_id, scope.active_generation_id AS generation_id
    FROM ingestion_scopes AS scope
    WHERE scope.scope_id > $1 AND scope.active_generation_id IS NOT NULL
    ORDER BY scope.scope_id
    LIMIT $2
),
owed AS (
    SELECT page.scope_id, page.generation_id
    FROM page
    WHERE EXISTS (
        SELECT 1 FROM fact_records AS fact
        WHERE fact.scope_id = page.scope_id
          AND fact.generation_id = page.generation_id
          AND fact.fact_kind = 'repository'
    )
      AND NOT EXISTS (
        SELECT 1 FROM activation_obligations AS obligation
        WHERE obligation.generation_id = page.generation_id
          AND obligation.scope_id = page.scope_id
    )
      AND NOT EXISTS (
        SELECT 1 FROM graph_projection_phase_state AS phase
        WHERE phase.scope_id = page.scope_id
          AND phase.acceptance_unit_id = page.scope_id
          AND phase.source_run_id = page.generation_id
          AND phase.generation_id = page.generation_id
          AND phase.keyspace = 'cross_repo_evidence'
          AND phase.phase = 'backward_evidence_committed'
    )
),
inserted AS (
    INSERT INTO activation_obligations (scope_id, generation_id, work_item_id)
    SELECT owed.scope_id, owed.generation_id,
        'projector_' || owed.scope_id || '_' || owed.generation_id
    FROM owed
    ON CONFLICT (scope_id, generation_id) DO NOTHING
    RETURNING 1
)
SELECT
    CASE WHEN (SELECT count(*) FROM page) < $2 THEN ''
         ELSE COALESCE((SELECT max(scope_id) FROM page), '') END,
    (SELECT count(*) FROM page),
    (SELECT count(*) FROM inserted)
`

// pruneQuery deletes up to $2 finished obligations whose finished_at is older
// than $1 milliseconds on the database clock. It locks only finished rows and
// skips any a concurrent replayed Finalize holds.
const pruneQuery = `
WITH doomed AS (
    SELECT generation_id, scope_id
    FROM activation_obligations
    WHERE state IN ('completed', 'obsolete')
      AND finished_at < clock_timestamp() - ($1::double precision * interval '1 millisecond')
    ORDER BY finished_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM activation_obligations AS obligation
USING doomed
WHERE obligation.generation_id = doomed.generation_id
  AND obligation.scope_id = doomed.scope_id
  AND obligation.state IN ('completed', 'obsolete')
`

// statsQuery counts obligations per state and reads the age of the oldest
// open obligation on the database clock. Open rows are bounded by the open
// partial index; finished rows by the prune.
const statsQuery = `
SELECT state, count(*),
    COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)
        FILTER (WHERE state IN ('pending', 'leased'))), 0)::double precision
FROM activation_obligations
GROUP BY state
`

// CatchUpPage reports one catch-up page.
type CatchUpPage struct {
	// NextCursor is the scope id to pass to the next CatchUp call; empty when
	// the page reached the end of ingestion_scopes.
	NextCursor string
	// Scanned counts the active scopes the page read.
	Scanned int
	// Inserted counts the obligations the page created.
	Inserted int
}

// Stats is a point-in-time census of the obligation table.
type Stats struct {
	// ByState counts rows per state; every state in AllStates is present.
	ByState map[State]int64
	// OldestOpenAge is the age of the oldest pending or leased obligation;
	// zero when none is open.
	OldestOpenAge time.Duration
}

// CatchUp owes obligations to one bounded page of already-active generations
// that lack both an obligation and their backward-evidence phase. Pass an
// empty cursor to start from the first scope.
func (s Store) CatchUp(ctx context.Context, cursor string, pageSize int) (CatchUpPage, error) {
	if pageSize <= 0 {
		return CatchUpPage{}, errors.New("catch up activation obligations: positive page size required")
	}
	var page CatchUpPage
	var scanned, inserted int64
	if _, err := queryOne(ctx, s.database, catchUpQuery, []any{cursor, pageSize},
		&page.NextCursor, &scanned, &inserted); err != nil {
		return CatchUpPage{}, fmt.Errorf("catch up activation obligations: %w", err)
	}
	page.Scanned, page.Inserted = int(scanned), int(inserted)
	return page, nil
}

// Prune deletes up to limit finished obligations older than retention.
func (s Store) Prune(ctx context.Context, retention time.Duration, limit int) (int, error) {
	if limit <= 0 || retention < 0 {
		return 0, errors.New("prune activation obligations: positive limit and non-negative retention required")
	}
	deleted, err := execRows(ctx, s.database, pruneQuery,
		float64(retention)/float64(time.Millisecond), limit)
	if err != nil {
		return 0, fmt.Errorf("prune activation obligations: %w", err)
	}
	return int(deleted), nil
}

// Stats reads the per-state census and the oldest open obligation age.
func (s Store) Stats(ctx context.Context) (Stats, error) {
	rows, err := s.database.QueryContext(ctx, statsQuery)
	if err != nil {
		return Stats{}, fmt.Errorf("read activation obligation stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stats := Stats{ByState: make(map[State]int64, len(AllStates))}
	for _, state := range AllStates {
		stats.ByState[state] = 0
	}
	var oldest float64
	for rows.Next() {
		var state string
		var count int64
		var age float64
		if err := rows.Scan(&state, &count, &age); err != nil {
			return Stats{}, fmt.Errorf("read activation obligation stats: scan: %w", err)
		}
		stats.ByState[State(state)] = count
		if age > oldest {
			oldest = age
		}
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("read activation obligation stats: %w", err)
	}
	stats.OldestOpenAge = time.Duration(oldest * float64(time.Second))
	return stats, nil
}
