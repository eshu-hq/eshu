// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Activation finalize outcomes, mirroring storage/postgres/activation. The
// runner adds lease_lost and error for a Finalize that returned an error.
const (
	ActivationOutcomeCompleted     = "completed"
	ActivationOutcomePhaseNotReady = "phase_not_ready"
	ActivationOutcomeWorkPending   = "work_pending"
	ActivationOutcomeObsolete      = "obsolete"
	ActivationOutcomeNotOwner      = "not_owner"
	ActivationOutcomeMissing       = "missing"
	// ActivationOutcomeInapplicable retires an obligation whose generation can
	// never carry a backward-evidence phase (no repository fact, or a
	// repository the shipped active-repository read does not map to it).
	ActivationOutcomeInapplicable = "inapplicable"
	activationOutcomeLeaseLost    = "lease_lost"
	activationOutcomeError        = "error"
)

const (
	defaultActivationLease           = 2 * time.Minute
	defaultActivationPollInterval    = 10 * time.Second
	defaultActivationMaxPerCycle     = 32
	defaultActivationCatchUpPageSize = 500
	defaultActivationPruneRetention  = 24 * time.Hour
	defaultActivationPruneLimit      = 500
)

// ActivationObligation is one claimed exact-generation activation obligation
// (#7584).
type ActivationObligation struct {
	ScopeID      string
	GenerationID string
	LeaseOwner   string
	LeaseToken   int64
	LeaseUntil   time.Time
	CreatedAt    time.Time
}

// ActivationFinalizeResult reports one Finalize: a closed outcome and the
// number of deployment_mapping rows whose wake committed.
type ActivationFinalizeResult struct {
	Outcome string
	Woken   int
}

// ActivationCatchUpPage reports one bounded catch-up page.
type ActivationCatchUpPage struct {
	NextCursor string
	Scanned    int
	Inserted   int
}

// ActivationStats is the per-status census of the obligation table.
type ActivationStats struct {
	ByState       map[string]int64
	OldestOpenAge time.Duration
}

// ActivationObligationStore is the storage port of the consumer. The
// Postgres implementation is storage/postgres/activation.RunnerStore.
type ActivationObligationStore interface {
	ClaimActivation(ctx context.Context, owner string, lease time.Duration) (*ActivationObligation, error)
	FinalizeActivation(ctx context.Context, work ActivationObligation) (ActivationFinalizeResult, error)
	RetireActivationInapplicable(ctx context.Context, work ActivationObligation) (ActivationFinalizeResult, error)
	CatchUpActivations(ctx context.Context, cursor string, pageSize int) (ActivationCatchUpPage, error)
	PruneActivations(ctx context.Context, retention time.Duration, limit int) (int, error)
	ActivationStats(ctx context.Context) (ActivationStats, error)
}

// ActivationMaintainer is the maintenance port: it must publish the exact
// generation's backward-evidence phase (and its evidence) when the
// generation can have one. The runner calls it only after a Finalize found
// the phase missing. Whole-corpus maintenance is a test control arm only; it
// is not an admissible shipped implementation (#7584 ruling D2).
type ActivationMaintainer interface {
	MaintainActivation(ctx context.Context, work ActivationObligation) error
}

// ActivationObligationRunnerConfig bounds the consumer.
type ActivationObligationRunnerConfig struct {
	// Owner is this process's lease owner; it must be unique per process.
	Owner string
	// Lease is how long one claim holds an obligation. An obligation left
	// open (phase not ready, work pending, failed callback) is retried after
	// its lease expires.
	Lease time.Duration
	// PollInterval is the wait after a cycle that claimed nothing.
	PollInterval time.Duration
	// Workers is the number of concurrent claim loops in this process; each
	// claims through SKIP LOCKED, so replicas and workers never share work.
	Workers int
	// MaxPerCycle bounds how many obligations one worker settles before the
	// cycle's housekeeping runs.
	MaxPerCycle int
	// CatchUpPageSize bounds the scopes one catch-up page reads.
	CatchUpPageSize int
	// PruneRetention keeps finished obligations this long before the prune.
	PruneRetention time.Duration
	// PruneLimit bounds the rows one prune deletes.
	PruneLimit int
}

func (c ActivationObligationRunnerConfig) withDefaults() ActivationObligationRunnerConfig {
	if c.Lease <= 0 {
		c.Lease = defaultActivationLease
	}
	if c.PollInterval <= 0 {
		c.PollInterval = defaultActivationPollInterval
	}
	if c.Workers <= 0 {
		c.Workers = 1
	}
	if c.MaxPerCycle <= 0 {
		c.MaxPerCycle = defaultActivationMaxPerCycle
	}
	if c.CatchUpPageSize <= 0 {
		c.CatchUpPageSize = defaultActivationCatchUpPageSize
	}
	if c.PruneRetention <= 0 {
		c.PruneRetention = defaultActivationPruneRetention
	}
	if c.PruneLimit <= 0 {
		c.PruneLimit = defaultActivationPruneLimit
	}
	return c
}

// ActivationObligationRunner is the resolution engine's consumer of
// activation obligations (#7584). Each worker claims one obligation at a
// time, finalizes it, calls the maintenance port only when the exact phase is
// missing, and finalizes again. Housekeeping (one catch-up page, one bounded
// prune, the census gauges) runs once per cycle on one worker. Nil disables
// the consumer; obligations then stay pending, one per activated generation,
// until retention removes their generation.
type ActivationObligationRunner struct {
	Store       ActivationObligationStore
	Maintainer  ActivationMaintainer
	Config      ActivationObligationRunnerConfig
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	mu     sync.Mutex
	cursor string
}

// Run drives every worker until ctx is cancelled.
func (r *ActivationObligationRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	cfg := r.Config.withDefaults()
	var wg sync.WaitGroup
	for worker := 0; worker < cfg.Workers; worker++ {
		wg.Add(1)
		go func(housekeeper bool) {
			defer wg.Done()
			r.runWorker(ctx, cfg, housekeeper)
		}(worker == 0)
	}
	wg.Wait()
	return nil
}

func (r *ActivationObligationRunner) runWorker(ctx context.Context, cfg ActivationObligationRunnerConfig, housekeeper bool) {
	for ctx.Err() == nil {
		processed := r.drain(ctx, cfg)
		if housekeeper {
			r.housekeep(ctx, cfg)
		}
		if processed > 0 {
			continue
		}
		timer := time.NewTimer(cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// RunOnce runs one bounded cycle on the calling goroutine: up to MaxPerCycle
// obligations, then housekeeping. It returns how many obligations it
// claimed.
func (r *ActivationObligationRunner) RunOnce(ctx context.Context) (int, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	cfg := r.Config.withDefaults()
	processed := r.drain(ctx, cfg)
	r.housekeep(ctx, cfg)
	return processed, nil
}

func (r *ActivationObligationRunner) validate() error {
	switch {
	case r.Store == nil:
		return errors.New("activation obligation runner: store is required")
	case r.Maintainer == nil:
		return errors.New("activation obligation runner: maintainer is required")
	case r.Config.Owner == "":
		return errors.New("activation obligation runner: lease owner is required")
	}
	return nil
}

// drain settles up to MaxPerCycle obligations and returns how many it claimed.
func (r *ActivationObligationRunner) drain(ctx context.Context, cfg ActivationObligationRunnerConfig) int {
	processed := 0
	for processed < cfg.MaxPerCycle && ctx.Err() == nil {
		work, err := r.Store.ClaimActivation(ctx, cfg.Owner, cfg.Lease)
		if err != nil {
			r.recordFailure(ctx, "claim", err, nil)
			return processed
		}
		if work == nil {
			return processed
		}
		processed++
		r.settle(ctx, *work)
	}
	return processed
}

func (r *ActivationObligationRunner) settle(ctx context.Context, work ActivationObligation) {
	if r.Instruments != nil && !work.CreatedAt.IsZero() {
		r.Instruments.ActivationObligationClaimAge.Record(ctx, time.Since(work.CreatedAt).Seconds())
	}
	result, err := r.Store.FinalizeActivation(ctx, work)
	if err == nil && result.Outcome == ActivationOutcomePhaseNotReady {
		r.recordOutcome(ctx, work, result)
		started := time.Now()
		maintainErr := r.Maintainer.MaintainActivation(ctx, work)
		var hold *ActivationHoldError
		r.recordMaintenance(ctx, time.Since(started), maintainErr)
		switch {
		case errors.Is(maintainErr, ErrActivationInapplicable):
			result, err = r.Store.RetireActivationInapplicable(ctx, work)
		case errors.As(maintainErr, &hold):
			// Held, not failed: the lease stays and the obligation is retried
			// at lease cadence; the epoch whole pass republishes the phase.
			r.recordHeld(ctx, work, hold.Reason(), maintainErr)
			return
		case maintainErr != nil:
			// The lease stays held; the obligation is retried after it expires.
			r.recordFailure(ctx, "maintenance", maintainErr, &work)
			return
		default:
			result, err = r.Store.FinalizeActivation(ctx, work)
		}
	}
	if err != nil {
		outcome := activationOutcomeError
		if errors.Is(err, ErrActivationLeaseLost) {
			outcome = activationOutcomeLeaseLost
		} else {
			r.recordFailure(ctx, "finalize", err, &work)
		}
		r.recordOutcome(ctx, work, ActivationFinalizeResult{Outcome: outcome})
		return
	}
	r.recordOutcome(ctx, work, result)
}

// housekeep runs one catch-up page, one bounded prune and the census.
func (r *ActivationObligationRunner) housekeep(ctx context.Context, cfg ActivationObligationRunnerConfig) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	cursor := r.cursor
	r.mu.Unlock()
	page, err := r.Store.CatchUpActivations(ctx, cursor, cfg.CatchUpPageSize)
	if err != nil {
		r.recordFailure(ctx, "catch_up", err, nil)
	} else {
		r.mu.Lock()
		r.cursor = page.NextCursor
		r.mu.Unlock()
		if r.Instruments != nil && page.Inserted > 0 {
			r.Instruments.ActivationObligationCatchUpInserted.Add(ctx, int64(page.Inserted))
		}
		if r.Logger != nil && page.Inserted > 0 {
			r.Logger.InfoContext(ctx, "activation obligation catch-up owed obligations",
				slog.Int("obligations_inserted", page.Inserted),
				slog.Int("scopes_scanned", page.Scanned),
				telemetry.PhaseAttr(telemetry.PhaseReduction))
		}
	}
	pruned, err := r.Store.PruneActivations(ctx, cfg.PruneRetention, cfg.PruneLimit)
	if err != nil {
		r.recordFailure(ctx, "prune", err, nil)
	} else if r.Instruments != nil && pruned > 0 {
		r.Instruments.ActivationObligationPruned.Add(ctx, int64(pruned))
	}
	stats, err := r.Store.ActivationStats(ctx)
	if err != nil {
		r.recordFailure(ctx, "stats", err, nil)
		return
	}
	if r.Instruments != nil {
		for state, count := range stats.ByState {
			r.Instruments.ActivationObligations.Record(ctx, count, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionStatus, state)))
		}
		r.Instruments.ActivationObligationOldestOpenAge.Record(ctx, stats.OldestOpenAge.Seconds())
	}
}

func (r *ActivationObligationRunner) recordOutcome(ctx context.Context, work ActivationObligation, result ActivationFinalizeResult) {
	if r.Instruments != nil {
		r.Instruments.ActivationObligationFinalizes.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, result.Outcome)))
		if result.Woken > 0 {
			r.Instruments.ActivationObligationWoken.Add(ctx, int64(result.Woken))
		}
	}
	if r.Logger == nil {
		return
	}
	attrs := append(telemetry.ScopeAttrs(work.ScopeID, work.GenerationID, ""),
		slog.String("outcome", result.Outcome),
		slog.Int("woken", result.Woken),
		slog.Int64("claim_token", work.LeaseToken),
		telemetry.PhaseAttr(telemetry.PhaseReduction))
	r.Logger.LogAttrs(ctx, slog.LevelInfo, "activation obligation finalized", attrs...)
}

func (r *ActivationObligationRunner) recordMaintenance(ctx context.Context, elapsed time.Duration, err error) {
	if r.Instruments == nil {
		return
	}
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	r.Instruments.ActivationObligationMaintenanceDuration.Record(ctx, elapsed.Seconds(),
		metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome)))
}

// recordHeld counts a held maintainer refusal under its own closed reason
// and logs it at INFO: holds are expected after a repository-catalog change
// or before the first whole pass, and clear when the epoch whole pass runs.
// eshu_dp_activation_obligation_oldest_open_age_seconds is the bound to alert
// on (one epoch pass latency plus one lease).
func (r *ActivationObligationRunner) recordHeld(ctx context.Context, work ActivationObligation, reason string, err error) {
	if r.Instruments != nil {
		r.Instruments.ActivationObligationFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, reason)))
	}
	if r.Logger == nil {
		return
	}
	attrs := append(telemetry.ScopeAttrs(work.ScopeID, work.GenerationID, ""),
		slog.String("reason", reason),
		slog.String("detail", err.Error()),
		slog.Int64("claim_token", work.LeaseToken),
		telemetry.PhaseAttr(telemetry.PhaseReduction))
	r.Logger.LogAttrs(ctx, slog.LevelInfo, "activation obligation held: maintenance refused", attrs...)
}

func (r *ActivationObligationRunner) recordFailure(ctx context.Context, reason string, err error, work *ActivationObligation) {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return // shutdown, not a failure
	}
	if r.Instruments != nil {
		r.Instruments.ActivationObligationFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, reason)))
	}
	if r.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		log.Err(fmt.Errorf("activation obligation %s: %w", reason, err)),
		telemetry.FailureClassAttr("activation_obligation_" + reason),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	if work != nil {
		attrs = append(attrs, telemetry.ScopeAttrs(work.ScopeID, work.GenerationID, "")...)
	}
	r.Logger.LogAttrs(ctx, slog.LevelError, "activation obligation step failed", attrs...)
}
