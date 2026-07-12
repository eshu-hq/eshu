// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// liveActivityQuery is the bounded live-activity read for the operations
// board (#5137): every in-flight work item joined to its originating
// ingestion scope for repo/collector identity. It fetches limit+1 rows so
// ReadLiveActivity can report `truncated` without a second COUNT query.
// lease_owner is COALESCEd to empty string so the Go scan target stays a plain string,
// matching the rest of this package's convention (see
// generationTransitionsQuery's current_active_generation_id); claim_until can
// be genuinely NULL (for example a retrying item not currently leased), so it
// scans into sql.NullTime.
//
// Performance Evidence: scratch Postgres 16 + migrations 001/002/005,
// synthetic corpus of 20k ingestion_scopes / 150k fact_work_items rows --
// normal shape (~1.9k in-flight rows) ran in 6.1ms via a Bitmap Index Scan on
// fact_work_items_status_idx (status, visible_at, updated_at) feeding a top-N
// heapsort under LIMIT; pathological shape (61k in-flight/retrying rows) ran
// in 12.3ms. Both are well inside the console's 10-12s poll budget.
const liveActivityQuery = `
SELECT w.work_item_id, w.stage, w.status, w.domain, COALESCE(w.lease_owner, '') AS lease_owner,
       w.claim_until, w.attempt_count, w.updated_at, w.created_at,
       s.scope_kind, s.collector_kind, s.source_system, s.source_key
FROM fact_work_items w
JOIN ingestion_scopes s ON s.scope_id = w.scope_id
WHERE w.status IN ('claimed', 'running', 'retrying')
ORDER BY w.updated_at DESC, w.work_item_id
LIMIT $1
`

// Bounds for the operator-supplied `limit` query parameter on
// GET /api/v0/status/operations. The query handler clamps to this range; the
// store itself falls back to LiveActivityDefaultLimit for a non-positive
// limit so a direct caller never issues an unbounded query.
const (
	LiveActivityDefaultLimit = 100
	LiveActivityMaxLimit     = 500
)

// LiveActivityStore reads the bounded in-flight work-item read model for the
// live operations board (#5137). It is a narrow, dedicated store rather than
// a StatusStore section because this query is bounded by an
// operator-supplied limit, unlike the fixed RawSnapshot shape the rest of the
// status surface composes.
type LiveActivityStore struct {
	queryer Queryer
	// Instruments is left nil by NewLiveActivityStore (matching StatusStore's
	// convention) so existing construction call sites stay source-compatible;
	// NewInstrumentedLiveActivityStore is the wiring entry point that wants
	// the duration/error signals.
	Instruments *telemetry.Instruments
}

// NewLiveActivityStore constructs a read-only live-activity store with no
// telemetry wired.
func NewLiveActivityStore(queryer Queryer) LiveActivityStore {
	return LiveActivityStore{queryer: queryer}
}

// NewInstrumentedLiveActivityStore constructs a live-activity store that
// records eshu_dp_status_operations_live_activity_query_duration_seconds and
// eshu_dp_status_operations_live_activity_query_errors_total on every read.
func NewInstrumentedLiveActivityStore(queryer Queryer, instruments *telemetry.Instruments) LiveActivityStore {
	return LiveActivityStore{queryer: queryer, Instruments: instruments}
}

// ReadLiveActivity returns up to limit in-flight work items (claimed,
// running, or retrying), ordered by most-recently-updated first, each joined
// to its originating repo/scope identity. A non-positive limit falls back to
// LiveActivityDefaultLimit. The bool result reports whether more matching
// rows existed than limit allowed through.
func (s LiveActivityStore) ReadLiveActivity(ctx context.Context, limit int) ([]statuspkg.LiveActivityRow, bool, error) {
	if limit <= 0 {
		limit = LiveActivityDefaultLimit
	}

	start := time.Now()
	rows, truncated, err := s.readLiveActivity(ctx, limit)
	s.recordOutcome(ctx, time.Since(start), err)
	return rows, truncated, err
}

func (s LiveActivityStore) readLiveActivity(ctx context.Context, limit int) ([]statuspkg.LiveActivityRow, bool, error) {
	if s.queryer == nil {
		return nil, false, fmt.Errorf("read live activity: queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, liveActivityQuery, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("read live activity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]statuspkg.LiveActivityRow, 0, limit)
	truncated := false
	for rows.Next() {
		if len(out) == limit {
			// The limit+1-th row only proves more rows exist; do not scan or
			// keep it.
			truncated = true
			break
		}
		var row statuspkg.LiveActivityRow
		var claimUntil sql.NullTime
		if scanErr := rows.Scan(
			&row.WorkItemID,
			&row.Stage,
			&row.Status,
			&row.Domain,
			&row.LeaseOwner,
			&claimUntil,
			&row.AttemptCount,
			&row.UpdatedAt,
			&row.CreatedAt,
			&row.ScopeKind,
			&row.CollectorKind,
			&row.SourceSystem,
			&row.SourceKey,
		); scanErr != nil {
			return nil, false, fmt.Errorf("read live activity: %w", scanErr)
		}
		if claimUntil.Valid {
			row.ClaimUntil = claimUntil.Time
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read live activity: %w", err)
	}
	return out, truncated, nil
}

// recordOutcome is nil-safe: when Instruments (or either instrument on it) is
// nil, recording is a no-op so LiveActivityStore stays usable without a
// meter provider wired, matching StatusStore's Instruments convention.
func (s LiveActivityStore) recordOutcome(ctx context.Context, duration time.Duration, err error) {
	if s.Instruments == nil {
		return
	}
	if s.Instruments.LiveActivityQueryDuration != nil {
		s.Instruments.LiveActivityQueryDuration.Record(ctx, duration.Seconds())
	}
	if err != nil && s.Instruments.LiveActivityQueryErrors != nil {
		s.Instruments.LiveActivityQueryErrors.Add(ctx, 1)
	}
}
