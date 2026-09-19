// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Read model states reported by FenceState.ReadModelState.
const (
	// StateNotInstalled means migration 109 has not been applied.
	StateNotInstalled = "not_installed"
	// StateBackfilling means the backfill marker does not exist yet.
	StateBackfilling = "backfilling"
	// StateFenced means the marker exists but at least one repository
	// carries a fence mark; unscoped reads stay on the graph until the
	// reducer's reconcile repairs it.
	StateFenced = "fenced"
	// StateReady means readers serve unscoped aggregates from the table.
	StateReady = "ready"
)

// fenceStateSQL reads the marker and the fence marks in one round trip.
// $1 marker name, $2 as-of time.
const fenceStateSQL = `
SELECT EXISTS (
           SELECT 1 FROM infra_resource_entity_backfill_markers WHERE marker_name = $1
       ),
       count(*),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM ($2::timestamptz - min(marked_at))), 0), 0)::float8
FROM infra_resource_entity_dirty_repos`

// FenceState is the infra read model's readiness inputs at one moment.
type FenceState struct {
	// Installed is false when migration 109 has not been applied.
	Installed bool
	// MarkerPresent reports the backfill marker.
	MarkerPresent bool
	// DirtyRepos is the number of repositories carrying a fence mark.
	DirtyRepos int64
	// OldestDirtyAge is the age of the oldest fence mark, 0 with none.
	OldestDirtyAge time.Duration
}

// ReadModelState names why readers do or do not use the table: one of
// StateNotInstalled, StateBackfilling, StateFenced, or
// StateReady.
func (s FenceState) ReadModelState() string {
	switch {
	case !s.Installed:
		return StateNotInstalled
	case !s.MarkerPresent:
		return StateBackfilling
	case s.DirtyRepos > 0:
		return StateFenced
	default:
		return StateReady
	}
}

// ReadFenceState reads the marker and the fence marks. A missing table
// (migration 109 not applied) returns a state with Installed false and no
// error.
func ReadFenceState(ctx context.Context, queryer db.Queryer, asOf time.Time) (FenceState, error) {
	rows, err := queryer.QueryContext(ctx, fenceStateSQL, BackfillMarker, asOf.UTC())
	if err != nil {
		if IsNotInstalled(err) {
			return FenceState{}, nil
		}
		return FenceState{}, fmt.Errorf("read infra inventory fence state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	state := FenceState{Installed: true}
	var ageSeconds float64
	if rows.Next() {
		if err := rows.Scan(&state.MarkerPresent, &state.DirtyRepos, &ageSeconds); err != nil {
			return FenceState{}, fmt.Errorf("scan infra inventory fence state: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return FenceState{}, fmt.Errorf("read infra inventory fence state: %w", err)
	}
	state.OldestDirtyAge = time.Duration(ageSeconds * float64(time.Second))
	return state, nil
}
