// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The stable-key fill's pause conditions and WAL pace (#10880 PR-C1k, arbiter
// ruling C1 section 1.6). The fill pauses when the replica replay lag is over the
// limit (default 60 s), when free disk is under the limit (default 100 GB), or while
// an index build is running (pg_stat_progress_create_index not empty), and it paces
// its writes to a WAL budget (default 8 GB an hour).
//
// Free disk cannot be read through SQL on a managed server, so the reading is a seam
// (StableKeyFillProbe.FreeDisk): with a limit set and no reading wired the fill
// pauses with reason disk_unreadable instead of running unwatched. An operator who
// watches disk outside the database sets the limit to 0 to turn that condition off.
// A condition that cannot be read never counts as clear.
//
// WAL pace is estimated, not measured from the log position: pg_current_wal_lsn()
// moves with every writer on a primary that already writes tens of GB an hour, so
// a delta around the fill's own transaction would throttle the fill for work it did
// not do. The estimate is rows filled times StableKeyFillPace.BytesPerRow, a figure
// the lab run (T6) calibrates.

const (
	// supplyChainImpactStableKeyFillReplayLagSQL reads the worst replay lag of any
	// attached replica, in seconds. A primary with no replica reads 0. Reading the
	// full view needs the pg_monitor or pg_read_all_stats role; a lag the role cannot
	// see comes back as NULL and reads 0, which the probe error path cannot catch, so
	// the deployment must grant the role.
	supplyChainImpactStableKeyFillReplayLagSQL = `
SELECT COALESCE(max(EXTRACT(EPOCH FROM replay_lag)), 0)::float8 FROM pg_stat_replication`

	// supplyChainImpactStableKeyFillIndexBuildSQL reports whether any index build or
	// reindex is in progress.
	supplyChainImpactStableKeyFillIndexBuildSQL = `
SELECT EXISTS (SELECT 1 FROM pg_stat_progress_create_index)`
)

// StableKeyFillLimits are the pause limits.
type StableKeyFillLimits struct {
	// MaxReplayLag pauses the fill above it; zero turns the condition off.
	MaxReplayLag time.Duration
	// MinFreeDiskBytes pauses the fill below it; zero turns the condition off.
	MinFreeDiskBytes int64
}

// StableKeyFillPauseState is one reading of the pause conditions.
type StableKeyFillPauseState struct {
	ReplayLag     time.Duration
	IndexBuilding bool
	// FreeDiskBytes is meaningful only when DiskKnown.
	FreeDiskBytes int64
	DiskKnown     bool
}

// PauseReason returns the telemetry reason to hold the fill back, or "" when it may
// run. Index builds always pause; the lag and disk conditions pause only when their
// limit is set.
func (l StableKeyFillLimits) PauseReason(s StableKeyFillPauseState) string {
	switch {
	case s.IndexBuilding:
		return telemetry.SupplyChainImpactStableKeyFillPauseIndexBuild
	case l.MaxReplayLag > 0 && s.ReplayLag > l.MaxReplayLag:
		return telemetry.SupplyChainImpactStableKeyFillPauseReplayLag
	case l.MinFreeDiskBytes > 0 && !s.DiskKnown:
		return telemetry.SupplyChainImpactStableKeyFillPauseDiskUnreadable
	case l.MinFreeDiskBytes > 0 && s.FreeDiskBytes < l.MinFreeDiskBytes:
		return telemetry.SupplyChainImpactStableKeyFillPauseFreeDisk
	}
	return ""
}

// StableKeyFillProbe reads the pause conditions.
type StableKeyFillProbe struct {
	DB db.Queryer
	// FreeDisk reports the free bytes of the database volume; ok is false when no
	// reading is available. Nil means no reading is wired.
	FreeDisk func(ctx context.Context) (bytes int64, ok bool, err error)
}

// Read reads every pause condition. Any read error is returned: the caller pauses
// with reason probe_error.
func (p StableKeyFillProbe) Read(ctx context.Context) (StableKeyFillPauseState, error) {
	var state StableKeyFillPauseState
	if p.DB == nil {
		return state, fmt.Errorf("stable key fill probe requires a database")
	}
	rows, err := p.DB.QueryContext(ctx, supplyChainImpactStableKeyFillReplayLagSQL)
	if err != nil {
		return state, fmt.Errorf("read replica replay lag: %w", err)
	}
	var seconds float64
	if rows.Next() {
		err = rows.Scan(&seconds)
	}
	closeErr := rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil || closeErr != nil {
		return state, fmt.Errorf("read replica replay lag: %w", firstError(err, closeErr))
	}
	state.ReplayLag = time.Duration(seconds * float64(time.Second))

	rows, err = p.DB.QueryContext(ctx, supplyChainImpactStableKeyFillIndexBuildSQL)
	if err != nil {
		return state, fmt.Errorf("read index build progress: %w", err)
	}
	if rows.Next() {
		err = rows.Scan(&state.IndexBuilding)
	}
	closeErr = rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil || closeErr != nil {
		return state, fmt.Errorf("read index build progress: %w", firstError(err, closeErr))
	}

	if p.FreeDisk != nil {
		bytes, ok, err := p.FreeDisk(ctx)
		if err != nil {
			return state, fmt.Errorf("read free disk: %w", err)
		}
		state.FreeDiskBytes, state.DiskKnown = bytes, ok
	}
	return state, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// StableKeyFillPace turns the rows a step filled into the wait that keeps the fill
// inside its WAL budget.
type StableKeyFillPace struct {
	// BytesPerHour is the WAL budget; zero or less gives an unbounded wait, so a
	// fill with no budget never runs.
	BytesPerHour int64
	// BytesPerRow is the estimated WAL one filled row writes.
	BytesPerRow int64
}

// Delay returns the minimum wait after a step that filled rows supports, so that
// rows*BytesPerRow bytes spread over the wait stay within BytesPerHour.
func (p StableKeyFillPace) Delay(rows int64) time.Duration {
	if rows <= 0 {
		return 0
	}
	if p.BytesPerHour <= 0 {
		return time.Hour
	}
	bytes := float64(rows) * float64(p.BytesPerRow)
	return time.Duration(bytes / float64(p.BytesPerHour) * float64(time.Hour))
}
