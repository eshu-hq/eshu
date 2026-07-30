// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	// configStateDriftRedriveCatchUpInterval bounds how often the catch-up
	// loop looks for due config_state_drift redrives. Independent of
	// configStateDriftRedriveAttemptSpacing: a shorter tick than the spacing
	// just means most ticks find nothing claimable, a cheap indexed no-op
	// query (see migrations/087_config_state_drift_runtime_redrive.sql).
	configStateDriftRedriveCatchUpInterval = time.Minute
	// configStateDriftRedriveAttemptSpacing is the gap the catch-up loop
	// schedules between one redrive attempt and the next for the SAME
	// (scope, generation) row -- independent of the tick interval above.
	configStateDriftRedriveAttemptSpacing = 5 * time.Minute
	// configStateDriftRedriveMaxAttempts bounds how many times a single
	// (scope, generation) gets reopened before the ledger row is claimed one
	// LAST time and DELETED (postgres.ConfigStateDriftRedriveStore.ClaimDue,
	// issue #5593 P1-B) -- a genuine "no config repo will ever own this
	// backend" outcome stops being retried, and its ledger row stops
	// existing, instead of accumulating forever. Paired with
	// reducer.TerraformConfigStateDriftHandler's own first-attempt delay
	// (defaultConfigStateDriftRedriveDelay, 5m, issue #5593 P1-A -- the
	// handler schedules a redrive ONLY when it actually observes
	// tfstatebackend.ErrNoConfigRepoOwnsBackend, not unconditionally) and
	// this package's configStateDriftRedriveAttemptSpacing (5m), the total
	// bounded recovery window is roughly 5m + (4-1)*5m = 20 minutes after
	// the rejection -- comfortably longer than a normal ingester poll/sync
	// cycle for the owning config-side repo to land and activate. Because
	// scheduling is now conditioned on the actual rejection (P1-A), this
	// cost applies only to generations that hit "no owner", not to every
	// runtime-triggered activation.
	configStateDriftRedriveMaxAttempts = 4
	// configStateDriftRedriveCatchUpBatchSize bounds how many due redrives
	// one tick claims, keeping each tick's own work bounded.
	configStateDriftRedriveCatchUpBatchSize = 100
)

// configStateDriftRedriveClaimer is the narrow surface the catch-up loop
// needs from postgres.ConfigStateDriftRedriveStore; tests substitute a fake.
type configStateDriftRedriveClaimer interface {
	ClaimDue(ctx context.Context, maxAttempts int, limit int, nextAttemptAt time.Time) ([]postgres.ConfigStateDriftRedriveClaim, error)
}

// configStateDriftRedriveReplayer is the narrow surface the catch-up loop
// needs from postgres.ReducerQueue to actually redrive a claimed work item;
// tests substitute a fake.
type configStateDriftRedriveReplayer interface {
	ReplayDomain(ctx context.Context, scopeID string, generationID string, domain reducer.Domain) (bool, error)
}

// runConfigStateDriftRedriveCatchUpLoop periodically reopens config_state_drift
// work items reducer.TerraformConfigStateDriftHandler scheduled for a bounded
// redrive after observing "no config repo owns this backend" (issue #5593
// P1-A/P1-1): a terraform_state_snapshot scope that activated before its
// owning config-side repo had synced and activated its own
// terraform_backends fact gets its config_state_drift work item reopened via
// ReplayDomain, giving the handler a fresh Handle() call once that
// correlation has had time to resolve. Runs until ctx is done, calling
// store.ClaimDue on every tick.
//
// A per-tick claim or replay error is logged and the loop continues -- this
// recovery pass must never take down the primary ingester composite runner,
// whose job is collection and source-local projection, not this catch-up.
func runConfigStateDriftRedriveCatchUpLoop(
	ctx context.Context,
	store configStateDriftRedriveClaimer,
	replayer configStateDriftRedriveReplayer,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(configStateDriftRedriveCatchUpInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runConfigStateDriftRedriveCatchUpTick(ctx, store, replayer, logger)
		}
	}
}

func runConfigStateDriftRedriveCatchUpTick(
	ctx context.Context,
	store configStateDriftRedriveClaimer,
	replayer configStateDriftRedriveReplayer,
	logger *slog.Logger,
) {
	nextAttemptAt := time.Now().UTC().Add(configStateDriftRedriveAttemptSpacing)
	claims, err := store.ClaimDue(ctx, configStateDriftRedriveMaxAttempts, configStateDriftRedriveCatchUpBatchSize, nextAttemptAt)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "config state drift redrive claim failed", "error", err)
		}
		return
	}
	for _, claim := range claims {
		reopened, replayErr := replayer.ReplayDomain(ctx, claim.ScopeID, claim.GenerationID, reducer.DomainConfigStateDrift)
		if replayErr != nil {
			if logger != nil {
				logger.ErrorContext(
					ctx, "config state drift redrive replay failed",
					"scope_id", claim.ScopeID, "generation_id", claim.GenerationID,
					"attempt", claim.AttemptCount, "error", replayErr,
				)
			}
			continue
		}
		if logger != nil {
			logger.InfoContext(
				ctx, "config state drift redrive attempted",
				"scope_id", claim.ScopeID, "generation_id", claim.GenerationID,
				"attempt", claim.AttemptCount, "reopened", reopened, "exhausted", claim.Exhausted,
			)
		}
	}
}

// runServiceAndJoinConfigStateDriftRedrive runs the primary ingester service
// to completion, then unconditionally cancels the redrive catch-up loop's
// context BEFORE joining its goroutine, mirroring
// cmd/projector's runServiceAndJoinRedrive (issue #5476 P0): calling cancel
// unconditionally, before Wait(), removes any dependency on defer ordering
// or on service.Run itself having canceled ctx (it does not, on every return
// path), so both the signal-shutdown path and a fatal-error return path
// converge on the same guaranteed-to-unblock cancel-then-join sequence.
func runServiceAndJoinConfigStateDriftRedrive(
	ctx context.Context,
	service serviceRunner,
	cancelRedrive context.CancelFunc,
	redriveWG *sync.WaitGroup,
) error {
	err := service.Run(ctx)
	cancelRedrive()
	redriveWG.Wait()
	return err
}

// serviceRunner is the narrow surface runServiceAndJoinConfigStateDriftRedrive
// needs. app.Application (main.go's `service`) satisfies it structurally;
// tests substitute a fake.
type serviceRunner interface {
	Run(context.Context) error
}
