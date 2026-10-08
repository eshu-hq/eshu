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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Producer settle outcomes, mirroring storage/postgres/activation. The
// runner adds lease_lost and error for a settle that returned an error.
const (
	ProducerActivationOutcomeCompleted    = "completed"
	ProducerActivationOutcomeObsolete     = "obsolete"
	ProducerActivationOutcomeNotOwner     = "not_owner"
	ProducerActivationOutcomeMissing      = "missing"
	ProducerActivationOutcomeInapplicable = "inapplicable"
	producerActivationOutcomeLeaseLost    = "lease_lost"
	producerActivationOutcomeError        = "error"
)

const (
	defaultProducerActivationLease          = 2 * time.Minute
	defaultProducerActivationPollInterval   = 10 * time.Second
	defaultProducerActivationMaxPerCycle    = 32
	defaultProducerActivationPruneRetention = 24 * time.Hour
	defaultProducerActivationPruneLimit     = 500
)

// ErrProducerActivationLeaseLost reports that the obligation lease expired
// while the settle held its transaction. The transaction rolled back, so the
// reopen did not survive, and the next owner repeats it.
var ErrProducerActivationLeaseLost = errors.New("producer activation obligation lease expired during settle")

// ErrProducerActivationSettleLockTimeout reports that the settle's
// lock_timeout expired while it waited for the scope or obligation row.
// Nothing was written.
var ErrProducerActivationSettleLockTimeout = errors.New("producer activation settle lock timeout")

// ProducerActivation is one claimed producer-activation obligation (#7635).
type ProducerActivation struct {
	ScopeID      string
	GenerationID string
	LeaseOwner   string
	LeaseToken   int64
	LeaseUntil   time.Time
	CreatedAt    time.Time
}

// ProducerActivationSettleResult reports one settle: a closed outcome and
// the reopened rows by consumer domain.
type ProducerActivationSettleResult struct {
	Outcome  string
	Reopened map[string]int
}

// ProducerActivationStats is the per-status census of the obligation table.
type ProducerActivationStats struct {
	ByState       map[string]int64
	OldestOpenAge time.Duration
}

// ProducerActivationStore is the storage port of the consumer. The Postgres
// implementation is storage/postgres.ProducerActivationRunnerStore. There is
// no catch-up: the activation package documents why a producer catch-up
// would re-owe every pruned generation.
type ProducerActivationStore interface {
	ClaimProducerActivation(ctx context.Context, owner string, lease time.Duration) (*ProducerActivation, error)
	SettleProducerActivation(ctx context.Context, work ProducerActivation) (ProducerActivationSettleResult, error)
	PruneProducerActivations(ctx context.Context, retention time.Duration, limit int) (int, error)
	ProducerActivationStats(ctx context.Context) (ProducerActivationStats, error)
}

// ProducerActivationRunnerConfig bounds the consumer.
type ProducerActivationRunnerConfig struct {
	// Owner is the lease owner; it must be process-unique.
	Owner string
	// Lease bounds one claim. Defaults to 2 minutes.
	Lease time.Duration
	// PollInterval is the idle wait between empty cycles. Defaults to 10s.
	PollInterval time.Duration
	// MaxPerCycle bounds one drain. Defaults to 32.
	MaxPerCycle int
	// PruneRetention holds finished rows before the prune deletes them.
	// Defaults to 24h.
	PruneRetention time.Duration
	// PruneLimit bounds one prune pass. Defaults to 500.
	PruneLimit int
	// Workers is the drain parallelism. Defaults to 1.
	Workers int
}

func (c ProducerActivationRunnerConfig) withDefaults() ProducerActivationRunnerConfig {
	if c.Lease <= 0 {
		c.Lease = defaultProducerActivationLease
	}
	if c.PollInterval <= 0 {
		c.PollInterval = defaultProducerActivationPollInterval
	}
	if c.MaxPerCycle <= 0 {
		c.MaxPerCycle = defaultProducerActivationMaxPerCycle
	}
	if c.PruneRetention <= 0 {
		c.PruneRetention = defaultProducerActivationPruneRetention
	}
	if c.PruneLimit <= 0 {
		c.PruneLimit = defaultProducerActivationPruneLimit
	}
	if c.Workers <= 0 {
		c.Workers = 1
	}
	return c
}

// ProducerActivationRunner is the resolution engine's consumer of
// producer-activation obligations (#7635). Each worker claims one obligation
// at a time and settles it: the dependent consumer items reopen and the
// obligation completes under the claim fence. Housekeeping (one bounded
// prune, the census gauges) runs once per cycle on one worker. Nil disables
// the consumer; obligations then stay pending, one per producer activation,
// until retention removes their generation.
type ProducerActivationRunner struct {
	Store       ProducerActivationStore
	Config      ProducerActivationRunnerConfig
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
	// Tracer, when set, opens one span per settle. Nil disables tracing.
	Tracer trace.Tracer
}

// Run drives every worker until ctx is cancelled.
func (r *ProducerActivationRunner) Run(ctx context.Context) error {
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

func (r *ProducerActivationRunner) runWorker(ctx context.Context, cfg ProducerActivationRunnerConfig, housekeeper bool) {
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
func (r *ProducerActivationRunner) RunOnce(ctx context.Context) (int, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	cfg := r.Config.withDefaults()
	processed := r.drain(ctx, cfg)
	r.housekeep(ctx, cfg)
	return processed, nil
}

func (r *ProducerActivationRunner) validate() error {
	switch {
	case r.Store == nil:
		return errors.New("producer activation runner: store is required")
	case r.Config.Owner == "":
		return errors.New("producer activation runner: lease owner is required")
	}
	return nil
}

// drain settles up to MaxPerCycle obligations and returns how many it claimed.
func (r *ProducerActivationRunner) drain(ctx context.Context, cfg ProducerActivationRunnerConfig) int {
	processed := 0
	for processed < cfg.MaxPerCycle && ctx.Err() == nil {
		work, err := r.Store.ClaimProducerActivation(ctx, cfg.Owner, cfg.Lease)
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

// housekeep runs one bounded prune and the census.
func (r *ProducerActivationRunner) housekeep(ctx context.Context, cfg ProducerActivationRunnerConfig) {
	if ctx.Err() != nil {
		return
	}
	pruned, err := r.Store.PruneProducerActivations(ctx, cfg.PruneRetention, cfg.PruneLimit)
	if err != nil {
		r.recordFailure(ctx, "prune", err, nil)
	} else if r.Instruments != nil && pruned > 0 {
		r.Instruments.ProducerActivationPruned.Add(ctx, int64(pruned))
	}
	stats, err := r.Store.ProducerActivationStats(ctx)
	if err != nil {
		r.recordFailure(ctx, "stats", err, nil)
		return
	}
	if r.Instruments != nil {
		for state, count := range stats.ByState {
			r.Instruments.ProducerActivations.Record(ctx, count, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionStatus, state)))
		}
		r.Instruments.ProducerActivationOldestOpenAge.Record(ctx, stats.OldestOpenAge.Seconds())
	}
}

// producerSettleVerdict is how one settle ended, for its span.
type producerSettleVerdict struct {
	outcome string
	failure string
	err     error
}

func (r *ProducerActivationRunner) settle(ctx context.Context, work ProducerActivation) {
	ctx, span := r.startSettleSpan(ctx, work)
	verdict := r.settleObligation(ctx, work)
	endProducerSettleSpan(span, verdict)
}

func (r *ProducerActivationRunner) settleObligation(ctx context.Context, work ProducerActivation) producerSettleVerdict {
	if r.Instruments != nil && !work.CreatedAt.IsZero() {
		r.Instruments.ProducerActivationClaimAge.Record(ctx, time.Since(work.CreatedAt).Seconds())
	}
	result, err := r.Store.SettleProducerActivation(ctx, work)
	if err != nil {
		if errors.Is(err, ErrProducerActivationLeaseLost) {
			r.recordOutcome(ctx, work, ProducerActivationSettleResult{Outcome: producerActivationOutcomeLeaseLost})
			return producerSettleVerdict{outcome: producerActivationOutcomeLeaseLost}
		}
		if errors.Is(err, ErrProducerActivationSettleLockTimeout) {
			// Expected contention (an ingestion commit or Ack held the scope
			// row past the lock timeout); nothing was written and the next
			// claimer settles it. Its own reason, at Warn, and no span error.
			r.recordFailureAt(ctx, slog.LevelWarn, producerActivationFailureSettleLockTimeout, err, &work)
			r.recordOutcome(ctx, work, ProducerActivationSettleResult{Outcome: producerActivationOutcomeError})
			return producerSettleVerdict{outcome: producerActivationOutcomeError, failure: producerActivationFailureSettleLockTimeout}
		}
		r.recordFailure(ctx, "settle", err, &work)
		r.recordOutcome(ctx, work, ProducerActivationSettleResult{Outcome: producerActivationOutcomeError})
		return producerSettleVerdict{outcome: producerActivationOutcomeError, failure: "settle", err: err}
	}
	r.recordOutcome(ctx, work, result)
	return producerSettleVerdict{outcome: result.Outcome}
}

// startSettleSpan starts the settle span. Without a tracer it installs a
// non-recording span, so nothing is ever written onto a span the caller's
// context already carries.
func (r *ProducerActivationRunner) startSettleSpan(ctx context.Context, work ProducerActivation) (context.Context, trace.Span) {
	if r.Tracer == nil {
		span := trace.SpanFromContext(context.Background())
		return trace.ContextWithSpan(ctx, span), span
	}
	return r.Tracer.Start(ctx, telemetry.SpanReducerProducerActivationSettle, trace.WithAttributes(
		attribute.String(telemetry.LogKeyScopeID, work.ScopeID),
		attribute.String(telemetry.LogKeyGenerationID, work.GenerationID),
		attribute.Int64("claim_token", work.LeaseToken),
	))
}

// endProducerSettleSpan records how the settle ended. A lease loss is a
// designed outcome, not a trace error; a failed settle is.
func endProducerSettleSpan(span trace.Span, verdict producerSettleVerdict) {
	span.SetAttributes(attribute.String("outcome", verdict.outcome))
	if verdict.failure != "" {
		span.SetAttributes(attribute.String("failure_reason", verdict.failure))
	}
	if verdict.err != nil {
		span.RecordError(verdict.err)
		span.SetStatus(codes.Error, verdict.failure)
	}
	span.End()
}

func (r *ProducerActivationRunner) recordOutcome(ctx context.Context, work ProducerActivation, result ProducerActivationSettleResult) {
	if r.Instruments != nil {
		r.Instruments.ProducerActivationSettles.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, result.Outcome)))
		for domain, reopened := range result.Reopened {
			if reopened > 0 {
				r.Instruments.ProducerActivationReopened.Add(ctx, int64(reopened), metric.WithAttributes(
					attribute.String(telemetry.MetricDimensionDomain, domain)))
			}
		}
	}
	if r.Logger == nil {
		return
	}
	reopened := 0
	for _, n := range result.Reopened {
		reopened += n
	}
	attrs := append(telemetry.ScopeAttrs(work.ScopeID, work.GenerationID, ""),
		slog.String("outcome", result.Outcome),
		slog.Int("reopened", reopened),
		slog.Int64("claim_token", work.LeaseToken),
		telemetry.PhaseAttr(telemetry.PhaseReduction))
	r.Logger.LogAttrs(ctx, slog.LevelInfo, "producer activation settled", attrs...)
}

// producerActivationFailureSettleLockTimeout is the failure reason of a
// settle whose lock_timeout expired (ErrProducerActivationSettleLockTimeout).
const producerActivationFailureSettleLockTimeout = "settle_lock_timeout"

func (r *ProducerActivationRunner) recordFailure(ctx context.Context, reason string, err error, work *ProducerActivation) {
	r.recordFailureAt(ctx, slog.LevelError, reason, err, work)
}

// recordFailureAt counts one consumer step failure under reason and logs it
// at level.
func (r *ProducerActivationRunner) recordFailureAt(ctx context.Context, level slog.Level, reason string, err error, work *ProducerActivation) {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return // shutdown, not a failure
	}
	if r.Instruments != nil {
		r.Instruments.ProducerActivationFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, reason)))
	}
	if r.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		log.Err(fmt.Errorf("producer activation %s: %w", reason, err)),
		telemetry.FailureClassAttr("producer_activation_" + reason),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	if work != nil {
		attrs = append(attrs, telemetry.ScopeAttrs(work.ScopeID, work.GenerationID, "")...)
	}
	r.Logger.LogAttrs(ctx, level, "producer activation step failed", attrs...)
}
