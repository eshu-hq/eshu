// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// liveScopeObservationsQuery reads one scope's live rows across every
// selector through the primary key's scope_id prefix. The live predicate is
// the SQL form of selection.Live; TestLiveFilterParityLive keeps the two
// equal at the window boundary.
const liveScopeObservationsQuery = `
SELECT selector_id, state, last_listed_at, state_since, state_cycle_count,
       evaluated_at, liveness_window_seconds
FROM repository_selection_observations
WHERE scope_id = $1
  AND evaluated_at + make_interval(secs => liveness_window_seconds) >= $2
ORDER BY selector_id
`

// ReadLiveScopeObservations returns scopeID's live selection observations,
// one per selector, ordered by selector id with times in UTC. A row is live
// while evaluated_at + liveness_window_seconds >= now. now is truncated to
// the microsecond, Postgres's timestamp precision, so the SQL filter and
// selection.Live compare the same instant. An expired row is never returned;
// an empty result means the scope has no live selection evidence.
func ReadLiveScopeObservations(ctx context.Context, queryer db.Queryer, scopeID string, now time.Time) ([]selection.Observation, error) {
	if queryer == nil {
		return nil, errors.New("read live repository selection observations: database is required")
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, errors.New("read live repository selection observations: scope id must not be blank")
	}
	rows, err := queryer.QueryContext(ctx, liveScopeObservationsQuery, scopeID, now.UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, fmt.Errorf("read live repository selection observations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var observations []selection.Observation
	for rows.Next() {
		var (
			observation   selection.Observation
			state         string
			lastListedAt  sql.NullTime
			windowSeconds int
		)
		if err := rows.Scan(
			&observation.SelectorID, &state, &lastListedAt, &observation.StateSince,
			&observation.StateCycleCount, &observation.EvaluatedAt, &windowSeconds,
		); err != nil {
			return nil, fmt.Errorf("read live repository selection observations: %w", err)
		}
		observation.State = selection.State(state)
		if lastListedAt.Valid {
			observation.LastListedAt = lastListedAt.Time.UTC()
		}
		observation.StateSince = observation.StateSince.UTC()
		observation.EvaluatedAt = observation.EvaluatedAt.UTC()
		observation.LivenessWindow = time.Duration(windowSeconds) * time.Second
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read live repository selection observations: %w", err)
	}
	return observations, nil
}
