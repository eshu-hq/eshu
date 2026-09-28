// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Defaults for Config. The slot count and the statement timeout live on the
// store (store.DefaultSlots, store.DefaultStatementTimeout).
const (
	DefaultPollInterval           = 5 * time.Second
	DefaultWorkers                = 4
	DefaultBackfillScopesPerCycle = 10
	DefaultMaxLinksPerScope       = 16
	orphanScopesPerCycle          = 100
	// orphanProbeInterval bounds how often the orphan probe runs. The probe
	// scans the link, activation and bucket-count tables (about 100 ms at
	// 25,000 links) and busy cycles follow each other at once, so it is
	// sampled at most once a minute; the gauge keeps the last value.
	orphanProbeInterval = time.Minute
)

// Linker links one activation of one scope per call and records counting
// failures. store.LinkWriter implements it.
type Linker interface {
	LinkNext(ctx context.Context, scopeID string) (store.LinkResult, error)
	RecordFailure(ctx context.Context, failure *store.FailureError, maxAttempts int) (store.FailureRecord, error)
}

// Journal journals activations and reads the ledger. store.JournalStore
// implements it.
type Journal interface {
	Journal(ctx context.Context, backfillScopes int) (store.JournalResult, error)
	BacklogScopes(ctx context.Context, limit int) ([]string, error)
	Stats(ctx context.Context) (store.LedgerStats, error)
	OrphanScopes(ctx context.Context, limit int) ([]string, error)
	DeleteOrphanScope(ctx context.Context, scopeID string) (bool, error)
	// Orphans runs the orphan probe (arbiter ruling arb-7127-3d, C5).
	Orphans(ctx context.Context) (store.LedgerOrphans, error)
}

// Config bounds one runner. Zero values take the package defaults.
type Config struct {
	PollInterval time.Duration
	// Workers is how many scopes link at once. Full links are further bounded
	// database-wide by the store's advisory slots.
	Workers int
	// BackfillScopesPerCycle bounds the scopes one journal pass backfills. A
	// negative value disables the backfill.
	BackfillScopesPerCycle int
	// MaxLinksPerScope bounds how many activations one scope links in one
	// cycle, so a long backlog on one scope does not starve the others.
	MaxLinksPerScope int
	// MaxAttempts is the counting-failure limit before an activation becomes
	// a link_poisoned break (store.DefaultMaxAttempts when below 1).
	MaxAttempts int
}

func (c Config) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return DefaultPollInterval
	}
	return c.PollInterval
}

func (c Config) workers() int {
	if c.Workers < 1 {
		return DefaultWorkers
	}
	return c.Workers
}

func (c Config) backfillScopes() int {
	switch {
	case c.BackfillScopesPerCycle < 0:
		return 0
	case c.BackfillScopesPerCycle == 0:
		return DefaultBackfillScopesPerCycle
	default:
		return c.BackfillScopesPerCycle
	}
}

func (c Config) maxLinksPerScope() int {
	if c.MaxLinksPerScope < 1 {
		return DefaultMaxLinksPerScope
	}
	return c.MaxLinksPerScope
}

// CycleResult summarizes one runner cycle. Breaks includes Poisoned.
type CycleResult struct {
	Journal  store.JournalResult
	Linked   int
	Breaks   int
	Poisoned int
	Retries  int
	Failures int
	Orphans  int
}

// Runner is the changed_since_link reducer domain. Each cycle journals
// activations, removes the ledger rows of deleted scopes, links each backlog
// scope with at most Workers scopes at once, and samples the backlog and
// state-table gauges. The store's per-scope cursor row is the writer fence
// and its advisory slots bound concurrent full links database-wide, so any
// number of runners (replicas) may run.
type Runner struct {
	Linker  Linker
	Journal Journal
	Config  Config
	Wait    func(context.Context, time.Duration) error
	// Classify returns the counting failure an error represents, or nil for
	// a non-counting or uncounted error. Nil uses CountingFailure. Tests plant
	// a wrong classifier here to prove the gates see it (G16b).
	Classify func(error) *store.FailureError

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	// lastOrphanProbe is when recordGauges last ran the orphan probe. Only
	// the goroutine running cycles touches it.
	lastOrphanProbe time.Time
}

// Run runs cycles until ctx is cancelled. A cycle that linked or broke a
// chain without any retry is followed at once by the next; an idle, retrying
// or failed cycle waits PollInterval.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			r.logReadError(ctx, "changed-since link cycle failed", err)
		}
		if err == nil && result.Linked+result.Breaks > 0 && result.Retries+result.Failures == 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if ctx.Err() != nil || errors.Is(waitErr, context.Canceled) {
				return nil
			}
			return fmt.Errorf("wait for changed-since link work: %w", waitErr)
		}
	}
}

// RunOnce runs one cycle: journal, orphan cleanup, the backlog, the gauges.
func (r *Runner) RunOnce(ctx context.Context) (CycleResult, error) {
	if err := r.validate(); err != nil {
		return CycleResult{}, err
	}
	var result CycleResult
	journal, err := r.Journal.Journal(ctx, r.Config.backfillScopes())
	if err != nil {
		return result, fmt.Errorf("journal activations: %w", err)
	}
	result.Journal = journal
	result.Orphans = r.deleteOrphans(ctx)

	scopes, err := r.Journal.BacklogScopes(ctx, r.Config.workers()*8)
	if err != nil {
		return result, fmt.Errorf("list backlog scopes: %w", err)
	}
	r.linkScopes(ctx, scopes, &result)
	r.recordGauges(ctx)
	r.logCycle(ctx, result)
	return result, nil
}

// linkScopes drains each scope's backlog with at most Workers scopes at once.
func (r *Runner) linkScopes(ctx context.Context, scopes []string, result *CycleResult) {
	work := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(r.Config.workers(), len(scopes)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for scopeID := range work {
				tally := r.drainScope(ctx, scopeID)
				mu.Lock()
				result.Linked += tally.Linked
				result.Breaks += tally.Breaks
				result.Poisoned += tally.Poisoned
				result.Retries += tally.Retries
				result.Failures += tally.Failures
				mu.Unlock()
			}
		}()
	}
	for _, scopeID := range scopes {
		if ctx.Err() != nil {
			break
		}
		work <- scopeID
	}
	close(work)
	wg.Wait()
}

// drainScope links one scope until it is idle, a link retries or fails, or
// MaxLinksPerScope activations are done. A retry or a failure stops the
// scope for this cycle: the cursor did not move, so the same activation is
// tried again next cycle.
func (r *Runner) drainScope(ctx context.Context, scopeID string) CycleResult {
	var tally CycleResult
	for range r.Config.maxLinksPerScope() {
		if ctx.Err() != nil {
			return tally
		}
		switch r.linkOne(ctx, scopeID) {
		case outcomeLinked:
			tally.Linked++
		case outcomeBreak:
			tally.Breaks++
		case outcomePoisoned:
			tally.Breaks++
			tally.Poisoned++
		case outcomeRetry:
			tally.Retries++
			return tally
		case outcomeFailed:
			tally.Failures++
			return tally
		case outcomeCanceled:
			return tally
		default:
			return tally
		}
	}
	return tally
}

// CountingFailure is the production classifier: the store's *FailureError
// is a counting failure; a *RetryError (non-counting) and any other error
// (begin, lock reads) are not.
func CountingFailure(err error) *store.FailureError {
	var failure *store.FailureError
	if errors.As(err, &failure) {
		return failure
	}
	return nil
}

func (r *Runner) deleteOrphans(ctx context.Context) int {
	orphans, err := r.Journal.OrphanScopes(ctx, orphanScopesPerCycle)
	if err != nil {
		r.logReadError(ctx, "changed-since orphan scan failed", err)
		return 0
	}
	deleted := 0
	for _, scopeID := range orphans {
		ok, err := r.Journal.DeleteOrphanScope(ctx, scopeID)
		if err != nil {
			r.logReadError(ctx, "changed-since orphan delete failed", err, slog.String("scope_id", scopeID))
			continue
		}
		if ok {
			deleted++
		}
	}
	return deleted
}

func (r *Runner) validate() error {
	if r == nil || r.Linker == nil || r.Journal == nil {
		return errors.New("changed-since link runner requires a linker and a journal")
	}
	return nil
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
