// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Trigger labels of a walker derive. They must equal the storage
// artifacts.Trigger values of the same name: the cmd/reducer adapter forwards
// them into DeriveRepository, and a parity test pins the equality. The leaf
// carries its own copies because it must never import storage.
const (
	// TriggerRepair is a walker derive of a repository the dirty join named.
	TriggerRepair = "repair"
	// TriggerBackfill is a walker derive of a repository the cursor walk
	// claimed.
	TriggerBackfill = "backfill"
)

// Derive outcomes that refuse without a statement, naming the storage
// artifacts.Outcome values of the same meaning: the process-start encoding or
// fence check failed, so the read model is off and no derive can run. The
// walker aborts the cycle on the first refusal instead of refusing every
// repository one by one.
const (
	outcomeEncodingUnsupported = "encoding_unsupported"
	outcomeFenceMissing        = "fence_missing"
	outcomeOK                  = "ok"
	outcomeSuperseded          = "superseded"
)

// StaleReasons is the closed reason set the dirty join reports, naming the
// storage artifacts.Reason values and the retry label: the walker sets the
// stale_repos gauge for every one of them each cycle, including zero, so a
// reason that drains back to zero does not keep a stale-high gauge. A parity
// test pins the equality with the storage constants.
var StaleReasons = []string{"missing", "failed", "stale_version", "stale_extractor", "retry"}

// DirtyRepo is one repository the dirty join named: work, or a skipped report.
type DirtyRepo struct {
	// RepoID is the content_files repository id.
	RepoID string
	// Reason is the dirty-join label: one of StaleReasons.
	Reason string
	// Skipped is true when the repository is over the failure limit for the
	// current extractor version. The walker counts it and never derives it;
	// its next content Write or an extractor change retries it.
	Skipped bool
}

// Lister finds the walker's repair work through the dirty join.
// cmd/reducer adapts storageartifacts.ListDirty to it.
type Lister interface {
	ListDirtyRepos(ctx context.Context, limit int) ([]DirtyRepo, error)
}

// Marker reads and records the backfill completion marker.
// cmd/reducer adapts the storage marker functions to it.
type Marker interface {
	BackfillRecorded(ctx context.Context) (bool, error)
	RecordBackfill(ctx context.Context) error
}

// Batch is what one bounded walk cycle did.
type Batch struct {
	// Derived counts repositories the cycle derived (refusals included).
	Derived int
	// Repaired, Matched, Superseded, Failed count derived repositories by
	// reconcile outcome.
	Repaired   int
	Matched    int
	Superseded int
	Failed     int
	// Skipped counts dirty-listed repositories the cycle did not derive.
	Skipped int
	// WalkWrapped is true when the claimed page reached the end of the walk.
	WalkWrapped bool
	// MarkerRecorded is true when the cycle recorded the backfill marker.
	MarkerRecorded bool
	// Refused is true when the cycle aborted on a read-model-off refusal.
	Refused bool
	// Duration is the cycle wall time.
	Duration time.Duration
}

// Runner repairs and backfills the stored config-artifact rows (#7126,
// protocol section 4). Each cycle derives the dirty join's repositories with
// trigger repair, then derives one claimed walk page with trigger backfill,
// running each group over K workers that partition by repo_id. When a claimed
// page reaches the end of the walk and nothing is dirty anymore, the cycle
// records the backfill marker: the first full walk is done.
//
// It never fails its caller: a failed derive is counted and recorded on the
// state row by the derive itself, and a failed cycle is logged and retried on
// the next interval. Derives of one repository in one process never overlap
// (Assign); across processes the derive's advisory lock and advance-only
// write keep two derives correct.
type Runner struct {
	Lister  Lister
	Claimer Claimer
	Deriver Deriver
	Marker  Marker
	Config  Config
	Wait    func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run walks until the context is canceled. A productive cycle loops
// immediately so a backlog drains; an empty or refused cycle waits the poll
// interval. A failed cycle is logged and retried on the next interval.
// Shutdown is prompt and silent: cancellation is not an error.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	cfg := r.Config.WithDefaults()
	for {
		if ctx.Err() != nil {
			return nil
		}
		batch, err := r.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			r.recordFailure(ctx, err)
		} else if batch.Derived == 0 || batch.Refused {
			if err := r.wait(ctx, cfg.PollInterval); err != nil {
				if contextDone(ctx, err) {
					return nil
				}
				return fmt.Errorf("wait for config artifact walk: %w", err)
			}
		}
	}
}

// RunOnce runs one bounded walk cycle: repair the dirty join's repositories,
// derive one claimed walk page, and record the backfill marker when the walk
// wraps with nothing dirty left.
func (r *Runner) RunOnce(ctx context.Context) (Batch, error) {
	if err := r.validate(); err != nil {
		return Batch{}, err
	}
	if ctx.Err() != nil {
		return Batch{}, nil
	}
	cfg := r.Config.WithDefaults()
	start := time.Now()
	var batch Batch

	markerComplete, err := r.Marker.BackfillRecorded(ctx)
	if err != nil {
		return Batch{}, fmt.Errorf("check config artifact backfill marker: %w", err)
	}
	dirty, err := r.Lister.ListDirtyRepos(ctx, cfg.PageBudget)
	if err != nil {
		return Batch{}, fmt.Errorf("list dirty config artifact repositories: %w", err)
	}
	r.recordStale(ctx, dirty)

	repairs := make([]string, 0, len(dirty))
	for _, item := range dirty {
		if item.Skipped {
			batch.Skipped++
			r.emitReconcile(ctx, telemetry.ConfigArtifactReconcileOutcomeSkipped)
			continue
		}
		repairs = append(repairs, item.RepoID)
	}
	repairSet := make(map[string]struct{}, len(repairs))
	for _, repoID := range repairs {
		repairSet[repoID] = struct{}{}
	}
	if r.deriveGroup(ctx, repairs, TriggerRepair, cfg.Workers, &batch) {
		batch.Refused = true
		batch.Duration = time.Since(start)
		r.logCycle(ctx, batch)
		return batch, nil
	}

	page, _, err := r.Claimer.ClaimPage(ctx, cfg.PageBudget)
	if err != nil {
		batch.Duration = time.Since(start)
		r.logCycle(ctx, batch)
		return batch, fmt.Errorf("claim config artifact walk page: %w", err)
	}
	wrapped := len(page) < cfg.PageBudget
	batch.WalkWrapped = wrapped
	walk := make([]string, 0, len(page))
	for _, repoID := range page {
		if _, ok := repairSet[repoID]; ok {
			continue
		}
		walk = append(walk, repoID)
	}
	if r.deriveGroup(ctx, walk, TriggerBackfill, cfg.Workers, &batch) {
		batch.Refused = true
		batch.Duration = time.Since(start)
		r.logCycle(ctx, batch)
		return batch, nil
	}

	if wrapped && !markerComplete {
		rest, err := r.Lister.ListDirtyRepos(ctx, 1)
		if err != nil {
			batch.Duration = time.Since(start)
			r.logCycle(ctx, batch)
			return batch, fmt.Errorf("relist dirty config artifact repositories: %w", err)
		}
		if len(rest) == 0 {
			if err := r.Marker.RecordBackfill(ctx); err != nil {
				batch.Duration = time.Since(start)
				r.logCycle(ctx, batch)
				return batch, fmt.Errorf("record config artifact backfill marker: %w", err)
			}
			batch.MarkerRecorded = true
		}
	}
	batch.Duration = time.Since(start)
	r.logCycle(ctx, batch)
	return batch, nil
}

// deriveGroup derives repoIDs with trigger over K workers, one repository per
// worker at a time, and counts every derive on derives_total, the duration
// histogram and reconcile_total. It returns true when a read-model-off
// refusal aborted the group: the caller stops the cycle.
func (r *Runner) deriveGroup(
	ctx context.Context,
	repoIDs []string,
	trigger string,
	workers int,
	batch *Batch,
) (aborted bool) {
	if len(repoIDs) == 0 {
		return false
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var refused atomic.Bool
	for _, part := range Assign(repoIDs, workers) {
		if len(part) == 0 {
			continue
		}
		wg.Add(1)
		go func(ids []string) {
			defer wg.Done()
			for _, repoID := range ids {
				if ctx.Err() != nil || refused.Load() {
					return
				}
				outcome, duration, adapterErr := r.deriveOne(ctx, repoID, trigger)
				mu.Lock()
				batch.Derived++
				if ctx.Err() != nil {
					// Shutdown landed mid-derive: count the work, record
					// nothing, so a shutdown wave never reads as an error
					// wave.
					mu.Unlock()
					return
				}
				r.emitDerive(ctx, trigger, deriveOutcomeLabel(outcome, adapterErr), duration)
				switch reconcileOutcome(trigger, outcome, adapterErr) {
				case telemetry.ConfigArtifactReconcileOutcomeRepaired:
					batch.Repaired++
				case telemetry.ConfigArtifactReconcileOutcomeMatch:
					batch.Matched++
				case telemetry.ConfigArtifactReconcileOutcomeSuperseded:
					batch.Superseded++
				default:
					batch.Failed++
				}
				r.emitReconcile(ctx, reconcileOutcome(trigger, outcome, adapterErr))
				if adapterErr == nil && isRefusal(outcome) {
					refused.Store(true)
					mu.Unlock()
					return
				}
				mu.Unlock()
			}
		}(part)
	}
	wg.Wait()
	return refused.Load()
}

// deriveOne runs one walker derive under the config_artifacts.derive span and
// returns the derive outcome for reconcile mapping. A transport error from
// the adapter (never from the production Deriver, which reports through
// Result) returns "" with the error.
func (r *Runner) deriveOne(ctx context.Context, repoID, trigger string) (string, time.Duration, error) {
	start := time.Now()
	deriveCtx := ctx
	var span trace.Span
	if r.Tracer != nil {
		deriveCtx, span = r.Tracer.Start(ctx, telemetry.SpanConfigArtifactsDerive)
		defer span.End()
	}
	outcome, err := r.Deriver.Derive(deriveCtx, repoID, trigger)
	duration := time.Since(start)
	if span != nil {
		span.SetAttributes(
			attribute.String("repo_id", repoID),
			telemetry.AttrTrigger(trigger),
			telemetry.AttrOutcome(outcome),
		)
		if err != nil {
			span.RecordError(err)
		}
	}
	return outcome, duration, err
}

// isRefusal reports whether outcome refuses without a statement because the
// read model is off in this process.
func isRefusal(outcome string) bool {
	return outcome == outcomeEncodingUnsupported || outcome == outcomeFenceMissing
}

// deriveOutcomeLabel maps an adapter result to the derives_total outcome
// label: the derive outcome, or error for a transport failure.
func deriveOutcomeLabel(outcome string, adapterErr error) string {
	if adapterErr != nil {
		return "error"
	}
	return outcome
}

// reconcileOutcome maps a derive to the reconcile_total outcome: ok repaired,
// a backfill walk that found a repository already current matched (the
// advance-only write rolled back with superseded), a repair superseded by a
// racing Write superseded, every failure error. A backfill superseded by a
// racing Write misreports match for one cycle; the repository stays dirty and
// the next cycle repairs it, so the label self-corrects.
func reconcileOutcome(trigger, outcome string, adapterErr error) string {
	if adapterErr != nil {
		return telemetry.ConfigArtifactReconcileOutcomeError
	}
	switch outcome {
	case outcomeOK:
		return telemetry.ConfigArtifactReconcileOutcomeRepaired
	case outcomeSuperseded:
		if trigger == TriggerBackfill {
			return telemetry.ConfigArtifactReconcileOutcomeMatch
		}
		return telemetry.ConfigArtifactReconcileOutcomeSuperseded
	default:
		return telemetry.ConfigArtifactReconcileOutcomeError
	}
}

// emitDerive counts one walker derive on derives_total and observes its wall
// time on the derive duration histogram. A nil Instruments records nothing,
// so wiring tests run without a meter.
func (r *Runner) emitDerive(ctx context.Context, trigger, outcome string, duration time.Duration) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.ConfigArtifactDerives.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrTrigger(trigger),
		telemetry.AttrOutcome(outcome),
	))
	r.Instruments.ConfigArtifactDeriveDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		telemetry.AttrTrigger(trigger),
	))
}

// emitReconcile counts one repository on reconcile_total. A nil Instruments
// records nothing.
func (r *Runner) emitReconcile(ctx context.Context, outcome string) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.ConfigArtifactReconcile.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrOutcome(outcome),
	))
}

// recordStale sets the stale_repos gauge for every dirty-join reason,
// including zero, so a drained reason does not keep a stale-high gauge. A
// nil Instruments records nothing.
func (r *Runner) recordStale(ctx context.Context, dirty []DirtyRepo) {
	if r.Instruments == nil {
		return
	}
	counts := make(map[string]int64, len(StaleReasons))
	for _, reason := range StaleReasons {
		counts[reason] = 0
	}
	for _, item := range dirty {
		counts[item.Reason]++
	}
	for reason, count := range counts {
		r.Instruments.ConfigArtifactStaleRepos.Record(ctx, count, metric.WithAttributes(
			telemetry.AttrReason(reason),
		))
	}
}

func (r *Runner) validate() error {
	switch {
	case r.Lister == nil:
		return errors.New("config artifact walker: dirty lister is required")
	case r.Claimer == nil:
		return errors.New("config artifact walker: page claimer is required")
	case r.Deriver == nil:
		return errors.New("config artifact walker: deriver is required")
	case r.Marker == nil:
		return errors.New("config artifact walker: backfill marker is required")
	default:
		return nil
	}
}

func (r *Runner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Runner) recordFailure(ctx context.Context, err error) {
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "config artifact walker cycle failed",
			slog.String("event_name", "config_artifacts.walker.cycle_failed"),
			log.Err(err),
			telemetry.FailureClassAttr("config_artifact_walker_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

// logCycle logs one cycle at DEBUG, or at INFO when it recorded the backfill
// marker. The counters and the stale gauge carry the per-cycle detail; the
// log carries the shape for operators without a metrics read.
func (r *Runner) logCycle(ctx context.Context, batch Batch) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("event_name", "config_artifacts.walker.cycle"),
		slog.Int("derived", batch.Derived),
		slog.Int("repaired", batch.Repaired),
		slog.Int("matched", batch.Matched),
		slog.Int("superseded", batch.Superseded),
		slog.Int("failed", batch.Failed),
		slog.Int("skipped", batch.Skipped),
		slog.Bool("walk_wrapped", batch.WalkWrapped),
		slog.Bool("marker_recorded", batch.MarkerRecorded),
		slog.Float64("duration_seconds", batch.Duration.Seconds()),
	}
	if batch.MarkerRecorded {
		r.Logger.InfoContext(ctx, "config artifact walker recorded the backfill marker", attrs...)
		return
	}
	r.Logger.DebugContext(ctx, "config artifact walker cycle done", attrs...)
}

// contextDone reports whether err (or ctx) signals cancellation. It
// duplicates retention.contextDone and infra.contextDone: the leaves stay
// independent so none imports another.
func contextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
}
