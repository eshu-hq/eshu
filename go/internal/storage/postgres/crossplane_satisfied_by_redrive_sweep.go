// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	crossplaneRedriveDefaultPageSize     = 500
	crossplaneRedriveDefaultLeaseTimeout = 10 * time.Minute
	// crossplaneRedriveDefaultCatchUpBatchSize bounds how many stale claims
	// one catch-up pass reclaims, keeping each pass's own work bounded (issue
	// #5476 P1-a).
	crossplaneRedriveDefaultCatchUpBatchSize = 50
)

// listActiveCrossplaneXRDsInGenerationQuery loads every active CrossplaneXRD
// content_entity fact for EXACTLY one XRD scope generation, fenced to that
// scope's CURRENT active_generation_id. An empty result means either the
// generation never carried an XRD, or (the design's fence (a)) the generation
// has since been superseded by a newer one -- both cases are a correct
// no-op for the caller, never an error.
const listActiveCrossplaneXRDsInGenerationQuery = `
SELECT fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND (fact.payload->>'entity_type' = 'CrossplaneXRD' OR fact.payload->>'entity_kind' = 'CrossplaneXRD')
`

const isCrossplaneXRDGenerationStillActiveQuery = `
SELECT EXISTS(
    SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 AND active_generation_id = $2
)
`

// crossplaneRedriveXRDJoinKey is the (group, claim_kind) identity one XRD in
// the source generation resolves Claims against. Mirrors
// reducer.crossplaneXRDJoinKey.
type crossplaneRedriveXRDJoinKey struct {
	group     string
	claimKind string
}

// CrossplaneRedriveSweepResult reports one Sweep call's outcome. Attempted is
// false when the sweep never started real work (no active XRD in the
// generation, or the row was already claimed/completed by another owner).
type CrossplaneRedriveSweepResult struct {
	Attempted       bool
	TargetsEnqueued int
	PagesProcessed  int
	// Outcome is a small fixed vocabulary suitable for a low-cardinality
	// telemetry label: no_active_xrd, already_in_progress, completed,
	// reclaimed_mid_sweep, sweep_error.
	Outcome string
}

const (
	crossplaneRedriveOutcomeNoActiveXRD       = "no_active_xrd"
	crossplaneRedriveOutcomeAlreadyInProgress = "already_in_progress"
	crossplaneRedriveOutcomeCompleted         = "completed"
	crossplaneRedriveOutcomeReclaimedMidSweep = "reclaimed_mid_sweep"
)

// CrossplaneRedriveIntentReplayer enqueues or reopens exactly one target
// Claim scope's SATISFIED_BY materialization intent. ReducerQueue implements
// this via ReplayCrossplaneSatisfiedByMaterialization.
type CrossplaneRedriveIntentReplayer interface {
	ReplayCrossplaneSatisfiedByMaterialization(ctx context.Context, targetScopeID, targetGenerationID string) (bool, error)
}

// CrossplaneSatisfiedByRedriveSweeper runs the durable, bounded, paged
// cross-scope Claim re-drive sweep for one XRD source-generation (issue
// #5476). It is deliberately NOT run inside the projector Ack transaction:
// each page of the fan-out commits its own reducer-intent enqueue/reopen
// independently, so a crash or transient error mid-sweep loses at most the
// unprocessed remainder of the current page.
//
// A live-trigger Sweep call that errors or whose process crashes mid-fan-out
// leaves crossplane_satisfied_by_redrive_state 'claimed' with an expiring
// lease. That row is only ever revisited by ANOTHER Sweep/SweepBatch attempt
// for the SAME (xrd_scope_id, xrd_generation_id) -- nothing re-triggers one
// automatically. SweepBatch is that recovery path: a periodic/startup
// catch-up caller (see cmd/projector's runCrossplaneRedriveCatchUpLoop) must
// invoke it regularly so a stuck claim is reclaimed once its lease expires
// and the remaining targets are re-driven, closing the exact unbounded
// false-negative window #5476 was filed to fix -- now via crash/error
// instead of ingestion order.
type CrossplaneSatisfiedByRedriveSweeper struct {
	// DB reads the XRD's own active generation state and the cross-scope
	// target-discovery pages.
	DB Queryer
	// State tracks durable claim/completion for the XRD generation being swept.
	State CrossplaneRedriveStateStore
	// TargetLedger records each target scope's already-redriven identity so
	// a later sweep for the same (group, claim_kind) skips it.
	TargetLedger CrossplaneRedriveTargetLedgerStore
	// Replayer enqueues or reopens each target scope's SATISFIED_BY intent.
	Replayer CrossplaneRedriveIntentReplayer
	// Owner identifies this process class for the claim lease (mirrors
	// ProjectorQueue/ReducerQueue's LeaseOwner). NOT used as a completion
	// fence -- MarkCompleted fences on the bumped claim_fencing_token instead,
	// since Owner is commonly a static string shared by every replica and the
	// catch-up scanner (see migration 076's doc comment).
	Owner string
	// LeaseDuration bounds how long a claimed sweep may run before another
	// invocation may reclaim it. Zero defaults to 10 minutes.
	LeaseDuration time.Duration
	// PageSize bounds the target-discovery query's keyset page size. Zero
	// defaults to 500.
	PageSize int
	// Instruments records low-cardinality sweep telemetry. Nil is safe (no-op).
	Instruments *telemetry.Instruments
}

func (s CrossplaneSatisfiedByRedriveSweeper) pageSize() int {
	if s.PageSize > 0 {
		return s.PageSize
	}
	return crossplaneRedriveDefaultPageSize
}

func (s CrossplaneSatisfiedByRedriveSweeper) leaseDuration() time.Duration {
	if s.LeaseDuration > 0 {
		return s.LeaseDuration
	}
	return crossplaneRedriveDefaultLeaseTimeout
}

func (s CrossplaneSatisfiedByRedriveSweeper) validate() error {
	if s.DB == nil {
		return errors.New("crossplane redrive sweeper database is required")
	}
	if s.Replayer == nil {
		return errors.New("crossplane redrive sweeper replayer is required")
	}
	return nil
}

// Sweep runs the cross-scope Claim re-drive fan-out for one XRD source
// generation. It is safe to call redundantly (from the live post-activation
// trigger and a startup/periodic catch-up scan both firing for the same
// generation): EnsureQueued/ClaimExact converge to exactly one active sweeper
// at a time via FOR UPDATE SKIP LOCKED, and a generation already recorded
// 'completed' returns a no-op on the very first claim attempt.
func (s CrossplaneSatisfiedByRedriveSweeper) Sweep(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
) (CrossplaneRedriveSweepResult, error) {
	if err := s.validate(); err != nil {
		return CrossplaneRedriveSweepResult{}, err
	}
	if xrdScopeID == "" || xrdGenerationID == "" {
		return CrossplaneRedriveSweepResult{}, errors.New("crossplane redrive sweep requires xrd scope id and generation id")
	}

	keys, err := s.loadActiveXRDJoinKeys(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		return CrossplaneRedriveSweepResult{}, err
	}
	if len(keys) == 0 {
		// No active XRD in this exact generation (never had one, or already
		// superseded -- fence (a)). Never enqueue a marker row for a
		// generation that needs no sweep.
		s.recordSweep(ctx, crossplaneRedriveOutcomeNoActiveXRD)
		return CrossplaneRedriveSweepResult{Outcome: crossplaneRedriveOutcomeNoActiveXRD}, nil
	}

	if err := s.State.EnsureQueued(ctx, xrdScopeID, xrdGenerationID); err != nil {
		return CrossplaneRedriveSweepResult{}, err
	}
	claimed, fencingToken, err := s.State.ClaimExact(ctx, xrdScopeID, xrdGenerationID, s.Owner, s.leaseDuration())
	if err != nil {
		return CrossplaneRedriveSweepResult{}, err
	}
	if !claimed {
		// Already claimed by a live owner, or already completed.
		s.recordSweep(ctx, crossplaneRedriveOutcomeAlreadyInProgress)
		return CrossplaneRedriveSweepResult{Outcome: crossplaneRedriveOutcomeAlreadyInProgress}, nil
	}

	return s.runClaimedFanOut(ctx, xrdScopeID, xrdGenerationID, fencingToken, keys)
}

// sweepJoinKey pages through every target scope matching one XRD (group,
// claim_kind) join key, enqueuing/reopening each newly-seen target's
// SATISFIED_BY intent exactly once (seenTargets dedupes across every join
// key in the same generation, since one Claim scope can match the same or a
// different XRD in the same generation only once per re-drive) and recording
// it in the target ledger so a later sweep for the SAME identity skips it.
// superseded reports the fence (a) check firing mid-sweep, so the caller
// stops processing further join keys.
func (s CrossplaneSatisfiedByRedriveSweeper) sweepJoinKey(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
	key crossplaneRedriveXRDJoinKey,
	seenTargets map[string]struct{},
	result *CrossplaneRedriveSweepResult,
) (bool, error) {
	after := ""
	pageSize := s.pageSize()
	for {
		stillActive, err := s.isXRDGenerationStillActive(ctx, xrdScopeID, xrdGenerationID)
		if err != nil {
			return false, err
		}
		if !stillActive {
			// Fence (a): the XRD generation was superseded mid-sweep. Stop
			// without resurrecting stale intents; the new generation's own
			// activation enqueues its own fresh sweep.
			return true, nil
		}

		// Fully drain and close the page's rows BEFORE issuing any write
		// (ReplayCrossplaneSatisfiedByMaterialization, RecordRedriven). s.DB
		// and s.Replayer commonly share one underlying connection pool in
		// production (cmd/projector wires both from the same *sql.DB);
		// calling a write while this SELECT's rows are still open would hold
		// this page's connection checked out and try to acquire a second one
		// for the write from the same pool -- a self-inflicted
		// pool-exhaustion deadlock under a small pool, and needless
		// connection pressure even under a larger one.
		page, err := s.fetchTargetScopePage(ctx, key, xrdScopeID, after, pageSize)
		if err != nil {
			return false, err
		}

		last := after
		for _, target := range page {
			last = target.scopeID
			if _, seen := seenTargets[target.scopeID]; seen {
				continue
			}
			if _, err := s.Replayer.ReplayCrossplaneSatisfiedByMaterialization(ctx, target.scopeID, target.generationID); err != nil {
				return false, fmt.Errorf("replay crossplane satisfied-by materialization for %q: %w", target.scopeID, err)
			}
			if err := s.TargetLedger.RecordRedriven(ctx, target.scopeID, key.group, key.claimKind); err != nil {
				return false, fmt.Errorf("record crossplane redrive target for %q: %w", target.scopeID, err)
			}
			seenTargets[target.scopeID] = struct{}{}
		}
		result.PagesProcessed++

		if len(page) < pageSize {
			return false, nil
		}
		after = last
	}
}

// crossplaneRedriveTargetScope is one row of
// listCrossplaneRedriveTargetScopesQuery's result: a target Claim scope and
// its current active generation.
type crossplaneRedriveTargetScope struct {
	scopeID      string
	generationID string
}

// fetchTargetScopePage runs and fully drains one page of the target-discovery
// query, closing its rows before returning so the caller never holds this
// SELECT's connection open while issuing a write.
func (s CrossplaneSatisfiedByRedriveSweeper) fetchTargetScopePage(
	ctx context.Context,
	key crossplaneRedriveXRDJoinKey,
	xrdScopeID string,
	after string,
	pageSize int,
) ([]crossplaneRedriveTargetScope, error) {
	rows, err := s.DB.QueryContext(ctx, listCrossplaneRedriveTargetScopesQuery,
		key.group, key.claimKind, xrdScopeID, after, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list crossplane redrive target scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := make([]crossplaneRedriveTargetScope, 0, pageSize)
	for rows.Next() {
		var target crossplaneRedriveTargetScope
		if scanErr := rows.Scan(&target.scopeID, &target.generationID); scanErr != nil {
			return nil, fmt.Errorf("scan crossplane redrive target scope: %w", scanErr)
		}
		page = append(page, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list crossplane redrive target scopes: %w", err)
	}
	return page, nil
}

func (s CrossplaneSatisfiedByRedriveSweeper) isXRDGenerationStillActive(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
) (bool, error) {
	rows, err := s.DB.QueryContext(ctx, isCrossplaneXRDGenerationStillActiveQuery, xrdScopeID, xrdGenerationID)
	if err != nil {
		return false, fmt.Errorf("check crossplane xrd generation still active: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("check crossplane xrd generation still active: %w", err)
		}
		return false, nil
	}
	var stillActive bool
	if err := rows.Scan(&stillActive); err != nil {
		return false, fmt.Errorf("scan crossplane xrd generation still active: %w", err)
	}
	return stillActive, nil
}

// loadActiveXRDJoinKeys loads every distinct (group, claim_kind) join key
// among the XRD generation's active CrossplaneXRD facts. An empty result
// means the generation carries no active XRD (never had one, or -- fence (a)
// -- has since been superseded), which the caller treats as a no-op.
func (s CrossplaneSatisfiedByRedriveSweeper) loadActiveXRDJoinKeys(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
) ([]crossplaneRedriveXRDJoinKey, error) {
	rows, err := s.DB.QueryContext(ctx, listActiveCrossplaneXRDsInGenerationQuery, xrdScopeID, xrdGenerationID)
	if err != nil {
		return nil, fmt.Errorf("list active crossplane xrds in generation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[crossplaneRedriveXRDJoinKey]struct{})
	var keys []crossplaneRedriveXRDJoinKey
	for rows.Next() {
		var raw []byte
		if scanErr := rows.Scan(&raw); scanErr != nil {
			return nil, fmt.Errorf("scan active crossplane xrd: %w", scanErr)
		}
		var payload map[string]any
		if jsonErr := json.Unmarshal(raw, &payload); jsonErr != nil {
			continue
		}
		group, claimKind := crossplaneRedriveXRDFields(payload)
		if group == "" || claimKind == "" {
			continue
		}
		key := crossplaneRedriveXRDJoinKey{group: group, claimKind: claimKind}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active crossplane xrds in generation: %w", err)
	}
	return keys, nil
}

// crossplaneRedriveXRDFields reads (spec.group, spec.claimNames.kind) from a
// CrossplaneXRD content_entity payload. Mirrors
// reducer.crossplaneXRDCandidateFromPayload's field paths exactly (nested
// under entity_metadata, per
// internal/collector/git_content_fact_envelopes.go's contentEntityFactEnvelope);
// duplicated locally rather than imported because the reducer package's
// helpers are unexported and this package must not reach into reducer
// internals. Keep both in sync if the parser's payload shape changes.
func crossplaneRedriveXRDFields(payload map[string]any) (group string, claimKind string) {
	metadata, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return "", ""
	}
	group, _ = metadata["group"].(string)
	claimKind, _ = metadata["claim_kind"].(string)
	return group, claimKind
}

func (s CrossplaneSatisfiedByRedriveSweeper) recordSweep(ctx context.Context, outcome string) {
	if s.Instruments == nil || s.Instruments.CrossplaneRedriveSweeps == nil {
		return
	}
	s.Instruments.CrossplaneRedriveSweeps.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrOutcome(outcome),
	))
}

func (s CrossplaneSatisfiedByRedriveSweeper) recordTargetsAndPages(ctx context.Context, targets, pages int) {
	if s.Instruments == nil {
		return
	}
	if s.Instruments.CrossplaneRedriveTargetsEnqueued != nil && targets > 0 {
		s.Instruments.CrossplaneRedriveTargetsEnqueued.Add(ctx, int64(targets))
	}
	if s.Instruments.CrossplaneRedrivePagesProcessed != nil && pages > 0 {
		s.Instruments.CrossplaneRedrivePagesProcessed.Add(ctx, int64(pages))
	}
}
