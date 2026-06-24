// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// ListGenerationLifecycle returns one bounded, ordered page of scope generation
// lifecycle rows for the supplied filter. The filter is normalized (selectors
// trimmed, limit clamped into range) before the read. The reader fetches
// filter.Limit+1 rows so the caller can detect truncation, then reports
// Truncated and trims the page back to the requested limit.
//
// The read is fully scoped by the filter predicates and capped by LIMIT, so it
// is safe to call without a scope selector for a bounded recent-history scan.
// Callers that supply a scope/repository/generation selector and receive zero
// rows must treat the result as not-found rather than confident emptiness.
func (s StatusStore) ListGenerationLifecycle(
	ctx context.Context,
	filter statuspkg.GenerationLifecycleFilter,
) (statuspkg.GenerationLifecyclePage, error) {
	if s.queryer == nil {
		return statuspkg.GenerationLifecyclePage{}, fmt.Errorf("queryer is required")
	}

	filter = filter.Normalize()
	fetch := filter.Limit + 1

	rows, err := s.queryer.QueryContext(
		ctx,
		listGenerationLifecycleQuery,
		filter.ScopeID,
		filter.Repository,
		filter.CollectorKind,
		filter.SourceSystem,
		filter.GenerationID,
		filter.Status,
		fetch,
	)
	if err != nil {
		return statuspkg.GenerationLifecyclePage{}, fmt.Errorf("list generation lifecycle: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]statuspkg.GenerationLifecycleRecord, 0, filter.Limit)
	for rows.Next() {
		record, scanErr := scanGenerationLifecycleRow(rows)
		if scanErr != nil {
			return statuspkg.GenerationLifecyclePage{}, fmt.Errorf("list generation lifecycle: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return statuspkg.GenerationLifecyclePage{}, fmt.Errorf("list generation lifecycle: %w", err)
	}

	truncated := len(records) > filter.Limit
	if truncated {
		records = records[:filter.Limit]
	}

	return statuspkg.GenerationLifecyclePage{
		Records:   records,
		Limit:     filter.Limit,
		Truncated: truncated,
	}, nil
}

func scanGenerationLifecycleRow(rows Rows) (statuspkg.GenerationLifecycleRecord, error) {
	var record statuspkg.GenerationLifecycleRecord
	var freshnessHint string
	var observedAt sql.NullTime
	var ingestedAt sql.NullTime
	var activatedAt sql.NullTime
	var supersededAt sql.NullTime
	var totalCount int64
	var outstandingCount int64
	var inFlightCount int64
	var retryingCount int64
	var succeededCount int64
	var failedCount int64
	var deadLetterCount int64
	var failureClass string
	var failureMessage string
	var failureWorkItemStatus string
	var failureObservedAt sql.NullTime

	if err := rows.Scan(
		&record.ScopeID,
		&record.GenerationID,
		&record.ScopeKind,
		&record.SourceSystem,
		&record.CollectorKind,
		&record.CurrentActiveGenerationID,
		&record.IsActive,
		&record.TriggerKind,
		&freshnessHint,
		&record.Status,
		&observedAt,
		&ingestedAt,
		&activatedAt,
		&supersededAt,
		&totalCount,
		&outstandingCount,
		&inFlightCount,
		&retryingCount,
		&succeededCount,
		&failedCount,
		&deadLetterCount,
		&failureClass,
		&failureMessage,
		&failureWorkItemStatus,
		&failureObservedAt,
	); err != nil {
		return statuspkg.GenerationLifecycleRecord{}, err
	}

	record.FreshnessHint = strings.TrimSpace(freshnessHint)
	record.CurrentActiveGenerationID = strings.TrimSpace(record.CurrentActiveGenerationID)
	record.ObservedAt = nullableLifecycleTimestamp(observedAt)
	record.IngestedAt = nullableLifecycleTimestamp(ingestedAt)
	record.ActivatedAt = nullableLifecycleTimestamp(activatedAt)
	record.SupersededAt = nullableLifecycleTimestamp(supersededAt)
	record.QueueStatus = statuspkg.GenerationQueueStatus{
		Total:       int(totalCount),
		Outstanding: int(outstandingCount),
		InFlight:    int(inFlightCount),
		Retrying:    int(retryingCount),
		Succeeded:   int(succeededCount),
		Failed:      int(failedCount),
		DeadLetter:  int(deadLetterCount),
	}

	if strings.TrimSpace(failureClass) != "" {
		record.LatestFailure = &statuspkg.GenerationLatestFailure{
			FailureClass:   strings.TrimSpace(failureClass),
			FailureMessage: strings.TrimSpace(failureMessage),
			WorkItemStatus: strings.TrimSpace(failureWorkItemStatus),
			ObservedAt:     nullableLifecycleTimestamp(failureObservedAt),
		}
	}

	return record, nil
}

func nullableLifecycleTimestamp(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return statuspkg.GenerationLifecycleTimestamp(value.Time)
}
