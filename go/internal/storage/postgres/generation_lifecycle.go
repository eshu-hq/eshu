// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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
		filter.Scoped,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
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

func scanGenerationLifecycleRow(rows db.Rows) (statuspkg.GenerationLifecycleRecord, error) {
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
	var failureDetails string

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
		&failureDetails,
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
			PriorFailure:   parsePriorFailure(failureDetails),
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

// priorFailureDetails is the part of failure_details the drilldown reads: the
// prior_failure object the supersede fold (#7320), the stale-scope reclaim and
// the operator note (#7388) write. The prior failure's own
// details text is deliberately not decoded.
type priorFailureDetails struct {
	PriorFailure *struct {
		Status         string `json:"status"`
		FailureClass   string `json:"failure_class"`
		FailureMessage string `json:"failure_message"`
		UpdatedAt      string `json:"updated_at"`
	} `json:"prior_failure"`
}

// parsePriorFailure returns the prior failure a row's failure_details
// carries, or nil. failure_details is free text or JSON, so anything that is not
// a JSON object with an object-valued prior_failure yields nil and no error: a
// non-JSON row must never fail the drilldown page (#7385).
func parsePriorFailure(details string) *statuspkg.GenerationPriorFailure {
	details = strings.TrimSpace(details)
	if details == "" || details[0] != '{' {
		return nil
	}
	var parsed priorFailureDetails
	if err := json.Unmarshal([]byte(details), &parsed); err != nil || parsed.PriorFailure == nil {
		return nil
	}
	prior := statuspkg.GenerationPriorFailure{
		Status:         strings.TrimSpace(parsed.PriorFailure.Status),
		FailureClass:   strings.TrimSpace(parsed.PriorFailure.FailureClass),
		FailureMessage: strings.TrimSpace(parsed.PriorFailure.FailureMessage),
		UpdatedAt:      strings.TrimSpace(parsed.PriorFailure.UpdatedAt),
	}
	if prior == (statuspkg.GenerationPriorFailure{}) {
		return nil
	}
	if at, err := time.Parse(time.RFC3339Nano, prior.UpdatedAt); err == nil {
		prior.UpdatedAt = statuspkg.GenerationLifecycleTimestamp(at)
	}
	return &prior
}
