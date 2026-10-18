// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// readRepositorySelectionCounts aggregates the live repository selection
// observations by state and, for not_listed rows, by upstream probe verdict
// (#8454): the aggregate status section behind Report.RepositorySelection.
// States counts every live row; Classifications counts only live not_listed
// rows, so a selected scope never dilutes a verdict share. An empty table
// reads as the zero section, which renders as absent.
func readRepositorySelectionCounts(ctx context.Context, queryer db.Queryer, asOf time.Time) (statuspkg.RepositorySelectionCounts, error) {
	rows, err := queryer.QueryContext(ctx, repositorySelectionCountsQuery, asOf)
	if err != nil {
		return statuspkg.RepositorySelectionCounts{}, fmt.Errorf("list repository selection counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := statuspkg.RepositorySelectionCounts{}
	for rows.Next() {
		var state, classification string
		var count int64
		if scanErr := rows.Scan(&state, &classification, &count); scanErr != nil {
			return statuspkg.RepositorySelectionCounts{}, fmt.Errorf("list repository selection counts: %w", scanErr)
		}
		if counts.States == nil {
			counts.States = map[string]int{}
			counts.Classifications = map[string]int{}
		}
		counts.States[state] += int(count)
		if state == string(selection.StateNotListed) {
			counts.Classifications[classification] += int(count)
		}
	}
	if err := rows.Err(); err != nil {
		return statuspkg.RepositorySelectionCounts{}, fmt.Errorf("list repository selection counts: %w", err)
	}
	return counts, nil
}
