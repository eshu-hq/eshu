// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// readSourcePacingStatus publishes per-source reducer backpressure for the
// coordinator status snapshot (#10882 AC6): the shared latch is truth for
// "slowed", and the grouped claimable read supplies the observation behind
// it. A source appears when it has claimable reducer work or an engaged
// latch. The read never advances the latch; consult paths own transitions.
func readSourcePacingStatus(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) ([]collector.SourcePacingSnapshot, error) {
	signals, err := readSourceClaimableSignals(ctx, queryer, asOf)
	if err != nil {
		return nil, fmt.Errorf("read source pacing signals: %w", err)
	}
	latches, err := readSourceBackpressureLatches(ctx, queryer)
	if err != nil {
		return nil, fmt.Errorf("read source pacing latches: %w", err)
	}
	rows := make([]collector.SourcePacingSnapshot, 0, len(signals))
	seen := make(map[string]bool, len(signals))
	for _, signal := range signals {
		seen[signal.SourceSystem] = true
		rows = append(rows, sourcePacingSnapshot(signal, latches[signal.SourceSystem]))
	}
	for source, latch := range latches {
		if latch.engaged && !seen[source] {
			rows = append(rows, sourcePacingSnapshot(
				workflow.SourceQueueSignal{SourceSystem: source}, latch))
		}
	}
	return collector.CloneSourcePacing(rows), nil
}

// sourcePacingSnapshot joins one claimable observation with its latch row
// into the published slowed state.
func sourcePacingSnapshot(signal workflow.SourceQueueSignal, latch sourceLatchState) collector.SourcePacingSnapshot {
	row := collector.SourcePacingSnapshot{
		SourceSystem:       signal.SourceSystem,
		OldestClaimableAge: signal.OldestClaimableAge,
		ClaimableDepth:     signal.ClaimableDepth,
	}
	if latch.engaged {
		row.Slowed = true
		row.Reason = workflow.SourceBackpressureReasonSourceBehind
		row.EngagedSince = latch.engagedAt
	}
	return row
}

// readSourceClaimableSignals runs the grouped claimable-signal query on a
// read-only queryer for status publication. The consult path uses the
// caching variant on WorkflowControlStore; both share the query constant
// so publication and pacing can never disagree about the signal shape.
func readSourceClaimableSignals(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) ([]workflow.SourceQueueSignal, error) {
	rows, err := queryer.QueryContext(ctx, sourceClaimableSignalQuery, asOf.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var signals []workflow.SourceQueueSignal
	for rows.Next() {
		var source string
		var ageSeconds float64
		var depth int64
		if err := rows.Scan(&source, &ageSeconds, &depth); err != nil {
			return nil, err
		}
		age := time.Duration(ageSeconds * float64(time.Second))
		if age < 0 {
			age = 0
		}
		signals = append(signals, workflow.SourceQueueSignal{
			SourceSystem:       source,
			OldestClaimableAge: age,
			ClaimableDepth:     depth,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}

// readSourceBackpressureLatches reads every reducer-source latch row on a
// read-only queryer for status publication.
func readSourceBackpressureLatches(
	ctx context.Context,
	queryer db.Queryer,
) (map[string]sourceLatchState, error) {
	rows, err := queryer.QueryContext(ctx, sourceBackpressureLatchQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	latches := make(map[string]sourceLatchState)
	for rows.Next() {
		var domain string
		var engaged bool
		var at sql.NullTime
		if err := rows.Scan(&domain, &engaged, &at); err != nil {
			return nil, err
		}
		row := sourceLatchState{engaged: engaged}
		if at.Valid {
			row.engagedAt = at.Time
		}
		latches[domain] = row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return latches, nil
}
