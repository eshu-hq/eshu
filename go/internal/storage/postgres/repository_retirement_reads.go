// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"

	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// ReadRetirementSummary reads the bounded tombstone aggregate behind the
// index-status repository_retirements block (#7766): per-state open-row
// counts, the oldest pending age, and the last completion. Counts only, no
// ids. Readmitted rows (readmitted_at set) never appear in any count.
func (s RetirementStore) ReadRetirementSummary(ctx context.Context) (statuspkg.RetirementSummary, error) {
	const counts = `SELECT state, count(*) FROM repository_retirements
		WHERE readmitted_at IS NULL GROUP BY state`
	rows, err := s.database.QueryContext(ctx, counts)
	if err != nil {
		return statuspkg.RetirementSummary{}, fmt.Errorf("retirement summary counts: %w", err)
	}
	summary := statuspkg.RetirementSummary{CountsByState: make(map[string]int64)}
	func() {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var state string
			var count int64
			if scanErr := rows.Scan(&state, &count); scanErr != nil {
				err = fmt.Errorf("retirement summary counts scan: %w", scanErr)
				return
			}
			summary.CountsByState[state] = count
		}
		err = rows.Err()
	}()
	if err != nil {
		return statuspkg.RetirementSummary{}, err
	}

	const ages = `SELECT min(requested_at) FILTER (WHERE state = 'pending'),
		max(retired_at) FILTER (WHERE state = 'complete')
		FROM repository_retirements WHERE readmitted_at IS NULL`
	ageRows, err := s.database.QueryContext(ctx, ages)
	if err != nil {
		return statuspkg.RetirementSummary{}, fmt.Errorf("retirement summary ages: %w", err)
	}
	defer func() { _ = ageRows.Close() }()
	if !ageRows.Next() {
		if err := ageRows.Err(); err != nil {
			return statuspkg.RetirementSummary{}, fmt.Errorf("retirement summary ages read: %w", err)
		}
		return summary, nil
	}
	var oldestPending, lastCompleted sql.NullTime
	if err := ageRows.Scan(&oldestPending, &lastCompleted); err != nil {
		return statuspkg.RetirementSummary{}, fmt.Errorf("retirement summary ages scan: %w", err)
	}
	if err := ageRows.Err(); err != nil {
		return statuspkg.RetirementSummary{}, fmt.Errorf("retirement summary ages rows: %w", err)
	}
	now := time.Now().UTC()
	if oldestPending.Valid {
		summary.HasPending = true
		summary.OldestPendingAgeSeconds = now.Sub(oldestPending.Time).Seconds()
		if summary.OldestPendingAgeSeconds < 0 {
			summary.OldestPendingAgeSeconds = 0
		}
	}
	if lastCompleted.Valid {
		summary.HasCompleted = true
		summary.LastCompletedAt = lastCompleted.Time.UTC()
	}
	return summary, nil
}

// ReadPageRetirements reads one repository list page's tombstone labels in
// a single bounded statement (#7766): the open or complete row per repo id,
// served by the repo_id-leading partial open-row index. Repos without a
// row are absent from the map, never null entries.
func (s RetirementStore) ReadPageRetirements(
	ctx context.Context,
	repoIDs []string,
) (map[string]contractrepository.PageRetirement, error) {
	labels := make(map[string]contractrepository.PageRetirement, len(repoIDs))
	if len(repoIDs) == 0 {
		return labels, nil
	}
	const page = `SELECT retirement_id, repo_id, scope_id, state, phase, retired_at
		FROM repository_retirements WHERE repo_id = ANY($1) AND readmitted_at IS NULL`
	rows, err := s.database.QueryContext(ctx, page, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement page read: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, repoID, scopeID, state, phase string
		var retiredAt sql.NullTime
		if err := rows.Scan(&id, &repoID, &scopeID, &state, &phase, &retiredAt); err != nil {
			return nil, fmt.Errorf("retirement page read scan: %w", err)
		}
		label := contractrepository.PageRetirement{
			State:        state,
			Phase:        phase,
			RetirementID: id,
		}
		if retiredAt.Valid {
			label.RetiredAt = retiredAt.Time.UTC()
			label.HasRetiredAt = true
		}
		labels[repoID] = label
	}
	return labels, rows.Err()
}
