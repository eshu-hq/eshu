// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimableReadySQL is the per-row takeability test the signal aggregates
// filter on (#11187): a row the claim's own readiness gate refuses is not
// takeable, so it is neither backlog nor lag the paced intake can clear: it
// waits on canonical phases the intake has to deliver, so counting it could
// hold a latch forever. The hashed NOT IN skips the gate for domains with no
// readiness requirement. It sits in the aggregate FILTERs, not the WHERE: a
// subplan in the WHERE flips the grouped plan to a spilling sort aggregate
// (235 ms to 610 ms at 10x replay scale); in the FILTERs the plan stays put.
//
// A row parked in reducer_waiting_items is not takeable either (#11187 R4-3):
// it stays 'retrying' and turns visible when its 10 minute backstop expires,
// but it waits on a dependency the paced intake cannot deliver. The test is the
// wait row itself (parked_wait, the lateral probe in the queries' FROM), not a
// failure_class or timestamp shortcut: a woken row has its wait row deleted and
// reopened_at set, is real work, and ages from the wake. A parked row is always
// retrying, so the status guard keeps every other row off the wait table: at K=0
// the table is never touched; each visible retrying row adds one primary-key
// lookup (about 2.5 us at 13,000 wait rows, 4 us at 200,000, growing with the
// index depth). Correlated copies, a hashed NOT IN and an unguarded lateral
// measured +18% to +157% on the bounded probe at 10x (R4-3 decision record).
var claimableReadySQL = "((work.domain NOT IN (SELECT domain FROM reducer_claim_readiness_requirements)\n" +
	"            OR " + reducerClaimReadinessGateSQL("work", "readiness_req", "readiness_phase") + ")\n" +
	"           AND parked_wait.parked IS NULL)"

// parkedWaitProbeSQL is the one wait-row probe the signal queries join: at
// most one lookup per scanned retrying row, none for any other status.
const parkedWaitProbeSQL = `LEFT JOIN LATERAL (
  SELECT TRUE AS parked
  FROM reducer_waiting_items AS wait
  WHERE work.status = 'retrying'
    AND wait.work_item_id = work.work_item_id
  LIMIT 1
) AS parked_wait ON TRUE
`

// sourceClaimableSignalQuery reads the per-source claimable reducer signal
// in one grouped statement (#10882 decision 1): for every source system, the
// age of its oldest claimable outstanding reducer row and its claimable
// depth. Claimable means active generation, pending or retrying, visible
// now, and passing the reducer claim's readiness gate, so deliberately gated
// future-visible rows and rows still waiting on canonical phases never
// throttle a source. Conflict-fenced rows stay counted: they wait on a holder
// the reducer itself finishes, so they drain without intake. Source
// attribution matches the queue observer: payload source_system, falling back
// to the scope's source_system.
//
// It uses the per-row active form, not the materialized one: the matching
// set is the bounded claimable backlog (reducer pending|retrying and
// visible), so like the graph-write-pressure count it probes scope and
// generation only for matching rows instead of building one scope-state
// row per ingestion scope (#6794 selective-probe precedent). The two forms
// select the same rows (TestActiveFactWorkItemsFormsSelectTheSameRows).
//
// The grouped form serves status publication (readSourcePacingStatus inside
// the coordinator snapshot), which needs every source and does not run per
// claim. The claim-hot consult path uses the bounded per-source probe
// (boundedSourceSignalQuery, S0 verdict): the grouped scan is the slow one, p50
// about 266 ms at 10x replay scale (87,130 claimable rows) against about 92 ms
// for the bounded probe over about 13k rows of one source, same session on a
// busy host. Read the ratio, not the milliseconds: both move with host load.
var sourceClaimableSignalQuery = `
WITH ` + activeFactWorkItemsPerRowCTE + `,
` + reducerClaimReadinessRequirementsCTE() + `
SELECT COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') AS source_system,
       COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(COALESCE(work.reopened_at, work.created_at)) FILTER (WHERE ` + claimableReadySQL + `))), 0) AS oldest_age_seconds,
       COUNT(*) FILTER (WHERE ` + claimableReadySQL + `) AS claimable_depth,
       COUNT(*) FILTER (WHERE COALESCE(work.reopened_at, work.created_at) < $2 AND ` + claimableReadySQL + `) AS rows_past_engage_age,
       COUNT(*) FILTER (WHERE COALESCE(work.reopened_at, work.created_at) < $3 AND ` + claimableReadySQL + `) AS rows_past_release_age
FROM active_fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
` + parkedWaitProbeSQL + `WHERE work.stage = 'reducer'
  AND work.status IN ('pending', 'retrying')
  AND (work.visible_at IS NULL OR work.visible_at <= $1)
GROUP BY 1
ORDER BY source_system
`

// familyPendingSourcesQuery names the source systems with pending workflow
// work in one collector family, so a pacing consult resolves
// (collector_kind, collector_instance_id) to the sources a grant would
// actually collect for.
const familyPendingSourcesQuery = `
SELECT DISTINCT source_system
FROM workflow_work_items
WHERE collector_kind = $1
  AND collector_instance_id = $2
  AND status = 'pending'
  AND source_system <> ''
ORDER BY source_system
`

// sourceBackpressureLatchQuery reads every reducer-source latch row. The
// row set is bounded by the source-system count, so one read serves every
// consult; a missing row reads as disengaged.
const sourceBackpressureLatchQuery = `
SELECT domain, engaged, engaged_at
FROM workflow_claim_pacing
WHERE signal = 'reducer_source'
`

// sourceSignalCache holds one grouped claimable-signal observation shared
// by a store's source-backpressure consults under SignalCacheTTL. The
// latch row stays authoritative in shared storage and is always read
// fresh; only the signal numbers may lag by the TTL.
type sourceSignalCache struct {
	at      time.Time
	signals []workflow.SourceQueueSignal
}

// sourceLatchState is one source's shared hysteresis row: whether slowing
// is engaged and since when. A missing row reads as disengaged.
type sourceLatchState struct {
	engaged   bool
	engagedAt time.Time
}

// CheckSourceBackpressure returns the claim-pacing verdict for one claim
// attempt under per-source reducer backpressure (#10882 decisions 1-3). It
// resolves the claiming family's pending sources, evaluates each against
// the claimable reducer signal and the shared latch, advances latches on
// transitions, and paces the grant only when every pending source is
// slowed: a family still serving a healthy source is never whole-family
// throttled, so one source's backlog cannot delay another's intake
// through a shared family.
//
// A signal or latch read failure returns the failure with an unpaced
// verdict: an unavailable signal is not evidence of a backlog, so callers
// fail open and claim normally.
func (s *WorkflowControlStore) CheckSourceBackpressure(
	ctx context.Context,
	selector workflow.ClaimSelector,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) (workflow.ClaimPacingDecision, error) {
	if err := s.validateSourceBackpressure(cfg); err != nil {
		return workflow.ClaimPacingDecision{}, err
	}
	if !cfg.Enabled() {
		return workflow.ClaimPacingDecision{}, nil
	}
	sources, err := s.familyPendingSources(ctx, selector)
	if err != nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("source backpressure family sources: %w", err)
	}
	if len(sources) == 0 {
		return workflow.ClaimPacingDecision{}, nil
	}
	verdicts, err := s.evaluateSources(ctx, sources, cfg, now, true)
	if err != nil {
		return workflow.ClaimPacingDecision{}, err
	}
	for _, source := range sources {
		if !verdicts[source].Slowed {
			return workflow.ClaimPacingDecision{}, nil
		}
	}
	first := verdicts[sources[0]]
	decision := workflow.ClaimPacingDecision{
		Paced:      true,
		Signal:     workflow.ClaimPacingSignalReducerSource,
		Domain:     first.SourceSystem,
		RetryAfter: first.RetryAfter,
		Stuck:      first.Stuck,
	}
	s.recordSourceBackpressureDelay(ctx, decision)
	return decision, nil
}

// CheckSourceBackpressureForSource returns the slowed-state verdict for one
// named source, advancing its shared latch on transitions. Admission paths
// that already know the intent batch's source (the ingester reducer
// admission writer) consult through here instead of the family resolution.
func (s *WorkflowControlStore) CheckSourceBackpressureForSource(
	ctx context.Context,
	sourceSystem string,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) (workflow.SourceBackpressureDecision, error) {
	sourceSystem = strings.TrimSpace(sourceSystem)
	if sourceSystem == "" {
		return workflow.SourceBackpressureDecision{}, fmt.Errorf("source backpressure source system is required")
	}
	if err := s.validateSourceBackpressure(cfg); err != nil {
		return workflow.SourceBackpressureDecision{SourceSystem: sourceSystem}, err
	}
	if !cfg.Enabled() {
		return workflow.SourceBackpressureDecision{SourceSystem: sourceSystem}, nil
	}
	verdicts, err := s.evaluateSources(ctx, []string{sourceSystem}, cfg, now, true)
	if err != nil {
		return workflow.SourceBackpressureDecision{SourceSystem: sourceSystem}, err
	}
	return verdicts[sourceSystem], nil
}

// SourcePacingStates publishes every signalled source's slowed state and
// the observation behind it (#10882 AC6), including sources whose latch is
// engaged but whose claimable queue has drained. It never advances the
// latch: publication is a read, and the consult paths own transitions.
func (s *WorkflowControlStore) SourcePacingStates(
	ctx context.Context,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) ([]workflow.SourcePacingState, error) {
	if err := s.validateSourceBackpressure(cfg); err != nil {
		return nil, err
	}
	signals, err := s.sourceClaimableSignals(ctx, cfg, now)
	if err != nil {
		return nil, fmt.Errorf("source pacing signals: %w", err)
	}
	latches, err := s.sourceBackpressureLatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("source pacing latches: %w", err)
	}
	bySource := make(map[string]workflow.SourceQueueSignal, len(signals))
	for _, signal := range signals {
		bySource[signal.SourceSystem] = signal
	}
	sources := make([]string, 0, len(bySource)+len(latches))
	for source := range bySource {
		sources = append(sources, source)
	}
	for source, latch := range latches {
		if latch.engaged {
			if _, seen := bySource[source]; !seen {
				sources = append(sources, source)
			}
		}
	}
	slices.Sort(sources)
	states := make([]workflow.SourcePacingState, 0, len(sources))
	for _, source := range sources {
		signal := bySource[source]
		signal.SourceSystem = source
		latch := latches[source]
		verdict := workflow.DecideSourceSlowed(
			cfg, signal, workflow.SourceBackpressureLatch{Engaged: latch.engaged},
			engagedFor(latch, now))
		state := workflow.SourcePacingState{
			SourceSystem:       source,
			Slowed:             verdict.Slowed,
			Reason:             verdict.Reason,
			OldestClaimableAge: signal.OldestClaimableAge,
			ClaimableDepth:     signal.ClaimableDepth,
			Stuck:              verdict.Stuck,
		}
		if latch.engaged {
			state.EngagedSince = latch.engagedAt
		}
		states = append(states, state)
	}
	return states, nil
}

// validateSourceBackpressure checks the store and config preconditions
// shared by every source-backpressure consult.
func (s *WorkflowControlStore) validateSourceBackpressure(cfg workflow.SourceBackpressureConfig) error {
	if s.database == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("source backpressure config: %w", err)
	}
	return nil
}

// evaluateSources computes the slowed verdict per source from the grouped
// signal and shared latches. When advance is set it writes latch
// transitions and records the transition telemetry; steady-state consults
// write nothing, so coordinators racing a transition converge on the same
// row through the shared upsert.
func (s *WorkflowControlStore) evaluateSources(
	ctx context.Context,
	sources []string,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
	advance bool,
) (map[string]workflow.SourceBackpressureDecision, error) {
	// The consult probes only the family's sources (S0 verdict): one
	// bounded per-source read each, never a whole-backlog grouped
	// scan on the claim hot path. The grouped read stays for the
	// publication path, which needs every source at once.
	bySource, err := s.sourceSignalsForSources(ctx, cfg, sources, now)
	if err != nil {
		return nil, fmt.Errorf("source backpressure signals: %w", err)
	}
	latches, err := s.sourceBackpressureLatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("source backpressure latches: %w", err)
	}
	verdicts := make(map[string]workflow.SourceBackpressureDecision, len(sources))
	for _, source := range sources {
		signal := bySource[source]
		latch := latches[source]
		verdict := workflow.DecideSourceSlowed(
			cfg, signal, workflow.SourceBackpressureLatch{Engaged: latch.engaged},
			engagedFor(latch, now))
		verdicts[source] = verdict
		if !advance || verdict.Slowed == latch.engaged {
			if advance {
				s.recordSourceBackpressureStuck(ctx, verdict)
			}
			continue
		}
		var engagedAt any
		if verdict.Slowed {
			engagedAt = now.UTC()
		}
		if _, err := s.database.ExecContext(ctx, claimPacingLatchUpsert,
			string(workflow.ClaimPacingSignalReducerSource), source, verdict.Slowed, engagedAt, now.UTC()); err != nil {
			return nil, fmt.Errorf("source backpressure latch advance: %w", err)
		}
		s.recordSourceBackpressureTransition(ctx, verdict)
	}
	return verdicts, nil
}

// engagedFor is how long a latch engagement has run at now. A disengaged or
// unstamped latch reports zero, so Stuck only trips on a real overdue
// engagement.
func engagedFor(latch sourceLatchState, now time.Time) time.Duration {
	if latch.engaged && !latch.engagedAt.IsZero() && now.After(latch.engagedAt) {
		return now.Sub(latch.engagedAt)
	}
	return 0
}

// familyPendingSources resolves one claiming family to the source systems
// with pending workflow work in it.
func (s *WorkflowControlStore) familyPendingSources(
	ctx context.Context,
	selector workflow.ClaimSelector,
) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, familyPendingSourcesQuery,
		string(selector.CollectorKind), selector.CollectorInstanceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sources []string
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

// sourceClaimableSignals reads the grouped per-source claimable reducer
// signal, reusing a cached observation younger than SignalCacheTTL. A zero
// TTL reads every time, which is what the proofs use for determinism.
func (s *WorkflowControlStore) sourceClaimableSignals(
	ctx context.Context,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) ([]workflow.SourceQueueSignal, error) {
	if cfg.SignalCacheTTL > 0 {
		s.pacingMu.Lock()
		cached := s.sourceSignals
		s.pacingMu.Unlock()
		if cached != nil && !now.Before(cached.at) && now.Sub(cached.at) < cfg.SignalCacheTTL {
			return cached.signals, nil
		}
	}
	rows, err := s.database.QueryContext(ctx, sourceClaimableSignalQuery, unpreparedSignalArgs(
		now.UTC(), now.UTC().Add(-cfg.EngageAge), now.UTC().Add(-cfg.ReleaseAge))...)
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
	if cfg.SignalCacheTTL > 0 {
		s.pacingMu.Lock()
		s.sourceSignals = &sourceSignalCache{at: now, signals: signals}
		s.pacingMu.Unlock()
	}
	return signals, nil
}

// sourceBackpressureLatches reads every reducer-source latch row. Each
// missing row reads as disengaged.
func (s *WorkflowControlStore) sourceBackpressureLatches(
	ctx context.Context,
) (map[string]sourceLatchState, error) {
	return readSourceBackpressureLatches(ctx, s.database)
}

// recordSourceBackpressureTransition emits the engagement telemetry for one
// latch transition on the existing claim-pacing metric family with
// pacing_signal="reducer_source" and domain=<source_system>, so an
// operator sees slowed engagements and releases beside the depth pacing
// series. Nil instruments are a no-op.
func (s *WorkflowControlStore) recordSourceBackpressureTransition(
	ctx context.Context,
	verdict workflow.SourceBackpressureDecision,
) {
	if s.Instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		telemetry.AttrPacingSignal(string(workflow.ClaimPacingSignalReducerSource)),
		telemetry.AttrDomain(verdict.SourceSystem),
	)
	if verdict.Slowed {
		if s.Instruments.WorkflowClaimPacingEngagedTotal != nil {
			s.Instruments.WorkflowClaimPacingEngagedTotal.Add(ctx, 1, attrs)
		}
		if s.Instruments.WorkflowClaimPacingActive != nil {
			s.Instruments.WorkflowClaimPacingActive.Add(ctx, 1, attrs)
		}
		return
	}
	if s.Instruments.WorkflowClaimPacingActive != nil {
		s.Instruments.WorkflowClaimPacingActive.Add(ctx, -1, attrs)
	}
}

// recordSourceBackpressureStuck keeps an overdue engagement loud: the stuck
// counter fires on every consult that finds the source still behind past
// its engagement bound. Nil instruments are a no-op.
func (s *WorkflowControlStore) recordSourceBackpressureStuck(
	ctx context.Context,
	verdict workflow.SourceBackpressureDecision,
) {
	if s.Instruments == nil || !verdict.Stuck {
		return
	}
	if s.Instruments.WorkflowClaimPacingStuckTotal == nil {
		return
	}
	s.Instruments.WorkflowClaimPacingStuckTotal.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrPacingSignal(string(workflow.ClaimPacingSignalReducerSource)),
		telemetry.AttrDomain(verdict.SourceSystem),
	))
}

// recordSourceBackpressureDelay records every paced family grant on the
// pacing delay histogram, labelled with the slowed source. Nil instruments
// are a no-op.
func (s *WorkflowControlStore) recordSourceBackpressureDelay(
	ctx context.Context,
	decision workflow.ClaimPacingDecision,
) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimPacingDelayDuration == nil {
		return
	}
	s.Instruments.WorkflowClaimPacingDelayDuration.Record(ctx, decision.RetryAfter.Seconds(),
		metric.WithAttributes(
			telemetry.AttrPacingSignal(string(workflow.ClaimPacingSignalReducerSource)),
			telemetry.AttrDomain(decision.Domain),
		))
}
