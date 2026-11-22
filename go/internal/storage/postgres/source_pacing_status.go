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
	config workflow.SourceBackpressureConfig,
) ([]collector.SourcePacingSnapshot, error) {
	latches, err := readSourceBackpressureLatches(ctx, queryer)
	if err != nil {
		return nil, fmt.Errorf("read source pacing latches: %w", err)
	}
	// The grouped claimable scan is the expensive read (#10882 S0: 120 to 230
	// ms at 10x replay scale), and publication only needs it when a source can
	// be slowed or observed: the leg is configured on this process, or some
	// latch is engaged (gating on config alone would hide an engaged latch on
	// a process without the thresholds). Otherwise the status read costs the
	// small latch read, which keeps a deployment that never enables the leg
	// off the whole claimable backlog.
	var signals []workflow.SourceQueueSignal
	if config.Enabled() || anyLatchEngaged(latches) {
		signals, err = readSourceClaimableSignals(ctx, queryer, asOf, config)
		if err != nil {
			return nil, fmt.Errorf("read source pacing signals: %w", err)
		}
	}
	rows := make([]collector.SourcePacingSnapshot, 0, len(signals))
	seen := make(map[string]bool, len(signals))
	for _, signal := range signals {
		seen[signal.SourceSystem] = true
		rows = append(rows, sourcePacingSnapshot(signal, latches[signal.SourceSystem], asOf, config))
	}
	for source, latch := range latches {
		if latch.engaged && !seen[source] {
			rows = append(rows, sourcePacingSnapshot(
				workflow.SourceQueueSignal{SourceSystem: source}, latch, asOf, config))
		}
	}
	return collector.CloneSourcePacing(rows), nil
}

// anyLatchEngaged reports whether any source's latch is engaged.
func anyLatchEngaged(latches map[string]sourceLatchState) bool {
	for _, latch := range latches {
		if latch.engaged {
			return true
		}
	}
	return false
}

// sourcePacingSnapshot joins one claimable observation with its latch row
// into the published slowed state. Slowed is the latch truth. Stuck is the
// consult verdict's own Stuck, taken from DecideSourceSlowed over the same
// observation (#10882 review R3-1), so the two cannot drift: an engagement
// past MaxEngagement is stuck only while DepthFloor claimable rows are still
// past the release age, and a latch the next consult will release because
// fewer rows remain is not published as stuck. A disabled config never marks
// stuck, and an unstamped engagement cannot prove overdue.
func sourcePacingSnapshot(
	signal workflow.SourceQueueSignal,
	latch sourceLatchState,
	asOf time.Time,
	config workflow.SourceBackpressureConfig,
) collector.SourcePacingSnapshot {
	row := collector.SourcePacingSnapshot{
		SourceSystem:       signal.SourceSystem,
		OldestClaimableAge: signal.OldestClaimableAge,
		ClaimableDepth:     signal.ClaimableDepth,
	}
	if latch.engaged {
		row.Slowed = true
		row.Reason = workflow.SourceBackpressureReasonSourceBehind
		row.EngagedSince = latch.engagedAt
		verdict := workflow.DecideSourceSlowed(
			config, signal, workflow.SourceBackpressureLatch{Engaged: true}, engagedFor(latch, asOf))
		row.Stuck = verdict.Slowed && verdict.Stuck
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
	config workflow.SourceBackpressureConfig,
) ([]workflow.SourceQueueSignal, error) {
	rows, err := queryer.QueryContext(ctx, sourceClaimableSignalQuery,
		asOf.UTC(), asOf.UTC().Add(-config.EngageAge), asOf.UTC().Add(-config.ReleaseAge))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var signals []workflow.SourceQueueSignal
	for rows.Next() {
		var source string
		var ageSeconds float64
		var depth, pastEngage, pastRelease int64
		if err := rows.Scan(&source, &ageSeconds, &depth, &pastEngage, &pastRelease); err != nil {
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
			RowsPastEngageAge:  pastEngage,
			RowsPastReleaseAge: pastRelease,
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
