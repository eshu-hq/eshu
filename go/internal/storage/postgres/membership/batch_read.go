// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// liveScopeObservationsBatchQuery reads one page's live observations in one
// statement (#8454): the same row shape as liveScopeObservationsQuery,
// keyed by any of the page's scope ids. The scope_id equality is the
// primary key's leading column, so the read stays a bounded PK probe per
// scope id instead of a scan; EXPLAIN at the 802-scope QA shape is a 0.3 ms
// sequential scan only because the whole table fits in 112 buffers, and a
// bitmap PK scan at 2.3 ms for a 500-id page at the 100k-scope shape (see
// the Performance Evidence note in this package's README). An empty scope id
// set reads nothing.
const liveScopeObservationsBatchQuery = `
SELECT scope_id, selector_id, state, last_listed_at, state_since,
       state_cycle_count, evaluated_at, liveness_window_seconds,
       upstream_classification, successor_slug, classified_at
FROM repository_selection_observations
WHERE scope_id = ANY($1)
  AND evaluated_at + make_interval(secs => liveness_window_seconds) >= $2
ORDER BY scope_id, selector_id
`

// ReadLiveScopeObservationsBatch reads the live observations of every scope
// id in scopeIDs in one statement, grouped by scope id. now is the read
// clock the caller already holds (never time.Now inside a read). An error
// fails the whole read: the caller never serves entries with evidence
// silently missing.
func ReadLiveScopeObservationsBatch(ctx context.Context, queryer db.Queryer, scopeIDs []string, now time.Time) (map[string][]selection.Observation, error) {
	if queryer == nil {
		return nil, errors.New("read live repository selection observations batch: queryer is required")
	}
	byScope := make(map[string][]selection.Observation, len(scopeIDs))
	if len(scopeIDs) == 0 {
		return byScope, nil
	}
	rows, err := queryer.QueryContext(ctx, liveScopeObservationsBatchQuery, scopeIDs, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("read live repository selection observations batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			scopeID        string
			observation    selection.Observation
			state          string
			lastListedAt   sql.NullTime
			windowSeconds  int
			classification sql.NullString
			successor      sql.NullString
			classifiedAt   sql.NullTime
		)
		if err := rows.Scan(
			&scopeID, &observation.SelectorID, &state, &lastListedAt, &observation.StateSince,
			&observation.StateCycleCount, &observation.EvaluatedAt, &windowSeconds,
			&classification, &successor, &classifiedAt,
		); err != nil {
			return nil, fmt.Errorf("read live repository selection observations batch: %w", err)
		}
		observation.State = selection.State(state)
		if lastListedAt.Valid {
			observation.LastListedAt = lastListedAt.Time.UTC()
		}
		observation.StateSince = observation.StateSince.UTC()
		observation.EvaluatedAt = observation.EvaluatedAt.UTC()
		observation.LivenessWindow = time.Duration(windowSeconds) * time.Second
		observation.Classification = selection.UpstreamClassification(classification.String)
		if successor.Valid {
			observation.SuccessorSlug = successor.String
		}
		if classifiedAt.Valid {
			observation.ClassifiedAt = classifiedAt.Time.UTC()
		}
		byScope[scopeID] = append(byScope[scopeID], observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read live repository selection observations batch: %w", err)
	}
	return byScope, nil
}

// LiveScopeObservationsBatchQuery exposes liveScopeObservationsBatchQuery so
// external tests can assert on its key and live filter.
const LiveScopeObservationsBatchQuery = liveScopeObservationsBatchQuery
