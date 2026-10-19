// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimPacingDepthQuery counts pending workflow work items grouped by
// collector family in one pass over the pending set. One statement serves
// every family's domain signal and sums the global ceiling, so a pacing
// consult costs one pending-set scan no matter how many families the
// dispatcher rotates across.
const claimPacingDepthQuery = `
SELECT collector_kind, collector_instance_id, COUNT(*)
FROM workflow_work_items
WHERE status = 'pending'
GROUP BY collector_kind, collector_instance_id
`

// claimPacingLatchQuery reads the shared hysteresis rows for one pacing
// consult: the claiming family's domain row and the global ceiling row. A
// missing row reads as disengaged, so the latch needs no seeding or
// backfill.
const claimPacingLatchQuery = `
SELECT signal, engaged, engaged_at
FROM workflow_claim_pacing
WHERE (signal = 'domain' AND domain = $1)
   OR (signal = 'global' AND domain = '')
`

// claimPacingLatchUpsert advances one latch row. Engagements and releases
// converge on the same row, so coordinators racing a transition agree
// instead of conflicting.
const claimPacingLatchUpsert = `
INSERT INTO workflow_claim_pacing AS latch (signal, domain, engaged, engaged_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (signal, domain)
DO UPDATE SET engaged = EXCLUDED.engaged,
              engaged_at = EXCLUDED.engaged_at,
              updated_at = EXCLUDED.updated_at
`

// claimPacingDepthCache holds one depth observation shared by a store's
// pacing consults. The pending-count scan costs milliseconds at tens of
// thousands of pending rows against a sub-millisecond claim, so production
// consults reuse an observation for DepthCacheTTL. The latch row stays
// authoritative in shared storage and is always read fresh; only the depth
// numbers may lag by the TTL.
type claimPacingDepthCache struct {
	at      time.Time
	global  int64
	domains map[string]int64
}

// CheckClaimPacing reads per-domain and global pending depth with the shared
// hysteresis latch, advances the latch on transitions, and returns the
// pacing verdict for one claim attempt.
//
// A depth or latch read failure returns the failure with an unpaced verdict:
// an unavailable signal is not evidence of a backlog, so pacing fails open
// and the caller claims normally. Callers must treat a non-nil error as
// "claim unpaced", never as "claim denied".
func (s *WorkflowControlStore) CheckClaimPacing(
	ctx context.Context,
	selector workflow.ClaimSelector,
	cfg workflow.ClaimPacingConfig,
	now time.Time,
) (workflow.ClaimPacingDecision, error) {
	if s.database == nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("workflow control store database is required")
	}
	if err := cfg.Validate(); err != nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("claim pacing config: %w", err)
	}
	if !cfg.Enabled() {
		return workflow.ClaimPacingDecision{}, nil
	}
	domainKey := string(selector.CollectorKind) + "/" + selector.CollectorInstanceID
	depths, err := s.claimPacingDepths(ctx, domainKey, cfg, now)
	if err != nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("claim pacing depths: %w", err)
	}
	latch, err := s.claimPacingLatch(ctx, domainKey)
	if err != nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("claim pacing latch: %w", err)
	}
	var engagedFor time.Duration
	if latch.verdict.Engaged && !latch.engagedAt.IsZero() && now.After(latch.engagedAt) {
		engagedFor = now.Sub(latch.engagedAt)
	}
	decision := workflow.DecideClaimPacing(cfg, depths, latch.verdict, engagedFor)
	decision.Domain = pacingDecisionDomain(decision.Signal, domainKey)
	if err := s.advanceClaimPacingLatch(ctx, domainKey, latch, decision, now); err != nil {
		return workflow.ClaimPacingDecision{}, fmt.Errorf("claim pacing latch advance: %w", err)
	}
	s.recordClaimPacing(ctx, domainKey, latch.verdict, decision)
	return decision, nil
}

// claimPacingLatchRow is one shared-storage latch row: whether the signal
// is engaged and since when. A missing row reads as disengaged.
type claimPacingLatchRow struct {
	engaged   bool
	engagedAt time.Time
}

// claimPacingLatchState is one consult's shared-storage latch read: the
// collapsed verdict the pacing decision consumes (the domain row wins when
// both claim engagement) plus every row the advance consults, so a switch
// onto an already-engaged row keeps its engaged_at instead of restarting
// the stuck bound.
type claimPacingLatchState struct {
	verdict   workflow.ClaimPacingLatch
	engagedAt time.Time
	rows      map[workflow.ClaimPacingSignal]claimPacingLatchRow
}

// recordClaimPacing emits the operator signals for one pacing verdict: the
// engagement counter and active gauge move only on latch transitions, the
// stuck counter fires on every overdue verdict so a wedged throttle stays
// loud, and the delay histogram records every paced grant. Nil instruments
// are a no-op, so binaries without a wired meter provider are unaffected.
func (s *WorkflowControlStore) recordClaimPacing(
	ctx context.Context,
	domainKey string,
	prior workflow.ClaimPacingLatch,
	decision workflow.ClaimPacingDecision,
) {
	if s.Instruments == nil {
		return
	}
	inst := s.Instruments
	priorOn := map[workflow.ClaimPacingSignal]bool{}
	if prior.Engaged {
		priorOn[prior.Signal] = true
	}
	nowOn := map[workflow.ClaimPacingSignal]bool{}
	if decision.Paced {
		nowOn[decision.Signal] = true
	}
	for _, signal := range []workflow.ClaimPacingSignal{workflow.ClaimPacingSignalDomain, workflow.ClaimPacingSignalGlobal} {
		if nowOn[signal] == priorOn[signal] {
			continue
		}
		// The global ceiling carries no domain label; the domain signal
		// names the saturated family so its engage and release deltas land
		// on one series.
		domain := ""
		if signal == workflow.ClaimPacingSignalDomain {
			domain = domainKey
		}
		attrs := claimPacingMetricAttrs(signal, domain)
		if nowOn[signal] {
			if inst.WorkflowClaimPacingEngagedTotal != nil {
				inst.WorkflowClaimPacingEngagedTotal.Add(ctx, 1, attrs)
			}
			if inst.WorkflowClaimPacingActive != nil {
				inst.WorkflowClaimPacingActive.Add(ctx, 1, attrs)
			}
			continue
		}
		if inst.WorkflowClaimPacingActive != nil {
			inst.WorkflowClaimPacingActive.Add(ctx, -1, attrs)
		}
	}
	if !decision.Paced {
		return
	}
	attrs := claimPacingMetricAttrs(decision.Signal, decision.Domain)
	if decision.Stuck && inst.WorkflowClaimPacingStuckTotal != nil {
		inst.WorkflowClaimPacingStuckTotal.Add(ctx, 1, attrs)
	}
	if inst.WorkflowClaimPacingDelayDuration != nil {
		inst.WorkflowClaimPacingDelayDuration.Record(ctx, decision.RetryAfter.Seconds(), attrs)
	}
}

// claimPacingMetricAttrs labels a pacing signal with its closed signal name
// and, for the domain signal, the saturated family as "kind/instance". The
// domain value space is the operator-configured family set; the global
// signal carries no domain label.
func claimPacingMetricAttrs(signal workflow.ClaimPacingSignal, domain string) metric.MeasurementOption {
	opts := []attribute.KeyValue{telemetry.AttrPacingSignal(string(signal))}
	if domain != "" {
		opts = append(opts, telemetry.AttrDomain(domain))
	}
	return metric.WithAttributes(opts...)
}

// pacingDecisionDomain names the saturated family for the domain signal and
// stays empty for the global ceiling.
func pacingDecisionDomain(signal workflow.ClaimPacingSignal, domainKey string) string {
	if signal == workflow.ClaimPacingSignalDomain {
		return domainKey
	}
	return ""
}

// claimPacingDepths returns the pending depth for one family and system-wide,
// reusing a cached observation younger than DepthCacheTTL. A zero TTL reads
// every time, which is what the pacing proofs use for determinism.
func (s *WorkflowControlStore) claimPacingDepths(
	ctx context.Context,
	domainKey string,
	cfg workflow.ClaimPacingConfig,
	now time.Time,
) (workflow.ClaimPacingDepths, error) {
	if cfg.DepthCacheTTL > 0 {
		s.pacingMu.Lock()
		cached := s.pacingDepths
		s.pacingMu.Unlock()
		if cached != nil && !now.Before(cached.at) && now.Sub(cached.at) < cfg.DepthCacheTTL {
			if domain, ok := cached.domains[domainKey]; ok {
				return workflow.ClaimPacingDepths{Domain: domain, Global: cached.global}, nil
			}
		}
	}
	rows, err := s.database.QueryContext(ctx, claimPacingDepthQuery)
	if err != nil {
		return workflow.ClaimPacingDepths{}, err
	}
	defer func() { _ = rows.Close() }()
	domains := make(map[string]int64)
	var global int64
	for rows.Next() {
		var kind, instance string
		var pending int64
		if err := rows.Scan(&kind, &instance, &pending); err != nil {
			return workflow.ClaimPacingDepths{}, err
		}
		domains[kind+"/"+instance] += pending
		global += pending
	}
	if err := rows.Err(); err != nil {
		return workflow.ClaimPacingDepths{}, err
	}
	depths := workflow.ClaimPacingDepths{Domain: domains[domainKey], Global: global}
	if cfg.DepthCacheTTL > 0 {
		s.pacingMu.Lock()
		s.pacingDepths = &claimPacingDepthCache{at: now, global: global, domains: domains}
		s.pacingMu.Unlock()
	}
	return depths, nil
}

// claimPacingLatch reads the shared hysteresis rows for one consult. Each
// missing row reads as disengaged.
func (s *WorkflowControlStore) claimPacingLatch(
	ctx context.Context,
	domainKey string,
) (claimPacingLatchState, error) {
	state := claimPacingLatchState{
		rows: map[workflow.ClaimPacingSignal]claimPacingLatchRow{},
	}
	rows, err := s.database.QueryContext(ctx, claimPacingLatchQuery, domainKey)
	if err != nil {
		return claimPacingLatchState{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var signal string
		var engaged bool
		var at sql.NullTime
		if err := rows.Scan(&signal, &engaged, &at); err != nil {
			return claimPacingLatchState{}, err
		}
		var name workflow.ClaimPacingSignal
		switch signal {
		case string(workflow.ClaimPacingSignalDomain):
			name = workflow.ClaimPacingSignalDomain
		case string(workflow.ClaimPacingSignalGlobal):
			name = workflow.ClaimPacingSignalGlobal
		default:
			continue
		}
		row := claimPacingLatchRow{engaged: engaged}
		if at.Valid {
			row.engagedAt = at.Time
		}
		state.rows[name] = row
		if !engaged {
			continue
		}
		// The domain row wins when both rows claim engagement: per-domain
		// depth is consulted first, and the advance below converges the
		// stale row back to disengaged.
		if !state.verdict.Engaged || name == workflow.ClaimPacingSignalDomain {
			state.verdict.Engaged = true
			state.verdict.Signal = name
			if at.Valid {
				state.engagedAt = at.Time
			}
		}
	}
	if err := rows.Err(); err != nil {
		return claimPacingLatchState{}, err
	}
	return state, nil
}

// advanceClaimPacingLatch writes the latch rows whose desired state changed.
// Steady-state consults write nothing; transitions converge racing
// coordinators on the same rows.
func (s *WorkflowControlStore) advanceClaimPacingLatch(
	ctx context.Context,
	domainKey string,
	prior claimPacingLatchState,
	decision workflow.ClaimPacingDecision,
	now time.Time,
) error {
	desired := map[workflow.ClaimPacingSignal]bool{}
	if decision.Paced {
		desired[decision.Signal] = true
	}
	for _, signal := range []workflow.ClaimPacingSignal{workflow.ClaimPacingSignalDomain, workflow.ClaimPacingSignalGlobal} {
		if desired[signal] == prior.rows[signal].engaged {
			continue
		}
		domain := ""
		if signal == workflow.ClaimPacingSignalDomain {
			domain = domainKey
		}
		// Writes happen only on transitions, so an engagement always
		// starts its stuck bound here, and steady-state consults leave
		// engaged_at alone. A re-engagement after release restarts it.
		// A switch onto an already-engaged row is not a transition: the
		// desired state matches storage, so no write restarts its bound.
		var engagedAt any
		if desired[signal] {
			engagedAt = now.UTC()
		}
		if _, err := s.database.ExecContext(ctx, claimPacingLatchUpsert,
			string(signal), domain, desired[signal], engagedAt, now.UTC()); err != nil {
			return err
		}
	}
	return nil
}
