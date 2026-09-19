// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// This file keeps the pre-#6794 standalone status reads as a test oracle.
// Each statement is composed from the same pieces activeWorkSummaryQuery uses
// and is byte-identical to the statement the status snapshot ran before the
// five reads were folded into one; the scanners are the original ones. The
// live differential test compares readActiveWorkSummary against them.

const (
	stageCountsQuery = `
WITH ` + activeFactWorkItemsCTE + `
` + stageCountsSelect + `
ORDER BY ` + stageCountsOrder + `
`
	domainBacklogQuery = `
WITH ` + activeFactWorkItemsCTE + `,
` + domainBacklogCTEs + `
` + domainBacklogSelect + `
ORDER BY ` + domainBacklogOrder + `
`
	queueSnapshotQuery = `
WITH ` + activeFactWorkItemsCTE + `
` + queueSnapshotSelect + `
`
)

var reducerConflictBlockageQuery = `
WITH ` + activeFactWorkItemsCTE + `,
` + reducerConflictBlockageCTEs + `
` + reducerConflictBlockageSelect + `
ORDER BY ` + reducerConflictBlockageOrder + `
LIMIT 10
`

const latestQueueFailureQuery = `
WITH ` + activeFactWorkItemsCTE + `
` + latestQueueFailureSelect + `
ORDER BY ` + latestQueueFailureOrder + `
LIMIT 1
`

// listStageCounts is the pre-#6794 standalone stage-counts read.
func listStageCounts(ctx context.Context, queryer db.Queryer) ([]statuspkg.StageStatusCount, error) {
	return scanStandaloneStageCounts(ctx, queryer)
}

func scanStandaloneStageCounts(ctx context.Context, queryer db.Queryer) ([]statuspkg.StageStatusCount, error) {
	rows, err := queryer.QueryContext(ctx, stageCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("list stage counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := []statuspkg.StageStatusCount{}
	for rows.Next() {
		var stage, state string
		var count int64
		if err := rows.Scan(&stage, &state, &count); err != nil {
			return nil, fmt.Errorf("list stage counts: %w", err)
		}
		counts = append(counts, statuspkg.StageStatusCount{Stage: stage, Status: state, Count: int(count)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stage counts: %w", err)
	}
	return counts, nil
}

func listDomainBacklogs(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) ([]statuspkg.DomainBacklog, error) {
	rows, err := queryer.QueryContext(ctx, domainBacklogQuery, asOf)
	if err != nil {
		return nil, fmt.Errorf("list domain backlogs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	backlogs := []statuspkg.DomainBacklog{}
	for rows.Next() {
		var domain string
		var outstandingCount int64
		var inFlightCount int64
		var retryingCount int64
		var deadLetterCount int64
		var failedCount int64
		var oldestOutstandingAgeSeconds float64
		if scanErr := rows.Scan(
			&domain,
			&outstandingCount,
			&inFlightCount,
			&retryingCount,
			&deadLetterCount,
			&failedCount,
			&oldestOutstandingAgeSeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("list domain backlogs: %w", scanErr)
		}
		backlogs = append(backlogs, statuspkg.DomainBacklog{
			Domain:      domain,
			Outstanding: int(outstandingCount),
			InFlight:    int(inFlightCount),
			Retrying:    int(retryingCount),
			DeadLetter:  int(deadLetterCount),
			Failed:      int(failedCount),
			OldestAge:   durationFromSeconds(oldestOutstandingAgeSeconds),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list domain backlogs: %w", err)
	}

	return backlogs, nil
}

func readQueueSnapshot(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) (statuspkg.QueueSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, queueSnapshotQuery, asOf)
	if err != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
		}
		return statuspkg.QueueSnapshot{}, nil
	}

	var totalCount int64
	var outstandingCount int64
	var pendingCount int64
	var inFlightCount int64
	var retryingCount int64
	var succeededCount int64
	var deadLetterCount int64
	var failedCount int64
	var provenanceEdgeIdentityUpgradeApplied bool
	var provenanceEdgeIdentityUpgradeRequired int64
	var oldestOutstandingAgeSeconds float64
	var overdueClaimCount int64
	if scanErr := rows.Scan(
		&totalCount,
		&outstandingCount,
		&pendingCount,
		&inFlightCount,
		&retryingCount,
		&succeededCount,
		&deadLetterCount,
		&failedCount,
		&provenanceEdgeIdentityUpgradeApplied,
		&provenanceEdgeIdentityUpgradeRequired,
		&oldestOutstandingAgeSeconds,
		&overdueClaimCount,
	); scanErr != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
	}

	return statuspkg.QueueSnapshot{
		Total:                                 int(totalCount),
		Outstanding:                           int(outstandingCount),
		Pending:                               int(pendingCount),
		InFlight:                              int(inFlightCount),
		Retrying:                              int(retryingCount),
		Succeeded:                             int(succeededCount),
		DeadLetter:                            int(deadLetterCount),
		Failed:                                int(failedCount),
		ProvenanceEdgeIdentityUpgradeApplied:  provenanceEdgeIdentityUpgradeApplied,
		ProvenanceEdgeIdentityUpgradeRequired: int(provenanceEdgeIdentityUpgradeRequired),
		OldestOutstandingAge:                  durationFromSeconds(oldestOutstandingAgeSeconds),
		OverdueClaims:                         int(overdueClaimCount),
	}, nil
}

// listReducerConflictBlockages reports reducer rows that are otherwise
// claimable but fenced by an active row in the same durable conflict key.
func listReducerConflictBlockages(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) ([]statuspkg.QueueBlockage, error) {
	rows, err := queryer.QueryContext(ctx, reducerConflictBlockageQuery, asOf)
	if err != nil {
		return nil, fmt.Errorf("list reducer conflict blockages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	blockages := []statuspkg.QueueBlockage{}
	for rows.Next() {
		var stage string
		var domain string
		var conflictDomain string
		var conflictKey string
		var blockedCount int64
		var oldestBlockedAgeSeconds float64
		if scanErr := rows.Scan(
			&stage,
			&domain,
			&conflictDomain,
			&conflictKey,
			&blockedCount,
			&oldestBlockedAgeSeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("list reducer conflict blockages: %w", scanErr)
		}
		blockages = append(blockages, statuspkg.QueueBlockage{
			Stage:          stage,
			Domain:         domain,
			ConflictDomain: conflictDomain,
			ConflictKey:    conflictKey,
			Blocked:        int(blockedCount),
			OldestAge:      durationFromSeconds(oldestBlockedAgeSeconds),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reducer conflict blockages: %w", err)
	}

	return blockages, nil
}

func readLatestQueueFailure(
	ctx context.Context,
	queryer db.Queryer,
) (*statuspkg.QueueFailureSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, latestQueueFailureQuery)
	if err != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read latest queue failure: %w", err)
		}
		return nil, nil
	}

	var snapshot statuspkg.QueueFailureSnapshot
	var updatedAt sql.NullTime
	if scanErr := rows.Scan(
		&snapshot.Stage,
		&snapshot.Domain,
		&snapshot.Status,
		&snapshot.WorkItemID,
		&snapshot.ScopeID,
		&snapshot.GenerationID,
		&snapshot.FailureClass,
		&snapshot.FailureMessage,
		&snapshot.FailureDetails,
		&updatedAt,
	); scanErr != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", err)
	}
	if updatedAt.Valid {
		snapshot.UpdatedAt = updatedAt.Time.UTC()
	}

	snapshot.Stage = strings.TrimSpace(snapshot.Stage)
	snapshot.Domain = strings.TrimSpace(snapshot.Domain)
	snapshot.Status = strings.TrimSpace(snapshot.Status)
	snapshot.WorkItemID = strings.TrimSpace(snapshot.WorkItemID)
	snapshot.ScopeID = strings.TrimSpace(snapshot.ScopeID)
	snapshot.GenerationID = strings.TrimSpace(snapshot.GenerationID)
	snapshot.FailureClass = strings.TrimSpace(snapshot.FailureClass)
	snapshot.FailureMessage = strings.TrimSpace(snapshot.FailureMessage)
	snapshot.FailureDetails = strings.TrimSpace(snapshot.FailureDetails)

	return &snapshot, nil
}
