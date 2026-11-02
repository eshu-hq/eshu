// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Maintainer for the #3389 supply-chain impact canonical winners read model. See
// docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// Correctness-first design choice: the maintainer reconciles the winners table
// with the current active impact-fact set by running the atomic, idempotent full
// resweep (ResweepWinners: recompute every winner, write only the rows that are
// new or changed, delete stale rows, stamp the watermark, all in one statement)
// on startup and on a fixed cadence. This deliberately avoids per-canonical_key
// dirty tracking because the atomic reconcile cannot miss a change class —
// generation-activation flips, tombstones, and new sources are all captured by
// recomputing from the current active set, removing the "missed dirty signal"
// correctness risk the incremental design carried. Incremental per-key recompute
// remains a future performance optimization. The read reports freshness from the
// singleton watermark, which every resweep stamps whether or not a winner row
// changed, so a cadence lag is never served as fresh truth. A winner row's own
// materialized_at means "last content change" and no reader depends on it (#9774).
//
// Conflict domain: the whole winners table during a resweep. A single-owner
// partition lease (partitionCount = 1) normally keeps one reducer instance
// resweeping at a time. The lease is not renewed during a resweep, so its TTL can
// lapse under a long statement and let a second instance claim it. The resweep's
// own single-flight lock covers that case: the second instance skips instead of
// comparing against a table the first has not committed, which could otherwise
// stamp a fresh watermark over a missing winner. The rebuild stays idempotent, so
// the next tick reconciles to the same state after any failed or skipped resweep.

const (
	// SupplyChainImpactWinnersDomain names the maintainer's single-owner lease
	// partition. It is not a shared-projection edge domain; it only scopes the
	// lease so one instance resweeps at a time.
	SupplyChainImpactWinnersDomain = "supply_chain_impact_winners"

	defaultSupplyChainImpactWinnersLeaseOwner = "supply-chain-impact-winners-maintainer"
	defaultSupplyChainImpactWinnersInterval   = 30 * time.Second
	defaultSupplyChainImpactWinnersLeaseTTL   = 60 * time.Second
	maxSupplyChainImpactWinnersBackoff        = 5 * time.Minute
)

// SupplyChainImpactWinnersRebuilder reconciles the winners table to the current
// active impact-fact set and reports what the resweep wrote. Implemented by
// postgres.SupplyChainImpactWinnersStore.ResweepWinners.
type SupplyChainImpactWinnersRebuilder interface {
	ResweepWinners(ctx context.Context, materializedAt any) (SupplyChainImpactWinnersResweep, error)
}

// SupplyChainImpactWinnersWatermarkReader reads the committed read watermark. ok
// is false until the first resweep commits.
type SupplyChainImpactWinnersWatermarkReader interface {
	Watermark(ctx context.Context) (at time.Time, ok bool, err error)
}

// SupplyChainImpactWinnersResweep is what one resweep did. Inserted, Updated and
// Deleted are the rows it wrote a version for; Unchanged is the count of current
// winners it compared and skipped.
type SupplyChainImpactWinnersResweep struct {
	Inserted  int64
	Updated   int64
	Deleted   int64
	Unchanged int64
	// PreviousWatermark is the watermark the resweep replaced. It is the zero
	// time when the table had never been swept.
	PreviousWatermark time.Time
	// Skipped is true when another resweep was in flight, so this one wrote
	// nothing, stamped nothing and left the watermark to that resweep.
	Skipped bool
	// Path is the path that ran: telemetry.SupplyChainImpactWinnersPathFull or
	// ...PathKeyed. It is empty for a result from a rebuilder that predates the
	// keyed resweep (#9805), which the maintainer reads as full.
	Path string
	// Identifiers is the number of dirty-log identifier rows a keyed tick
	// consumed and Keys the number of canonical keys it recomputed. They are zero
	// on the full path.
	Identifiers int64
	Keys        int64
	// TriggerCaptured is the consumed dirty-log rows by the capture sources
	// whose owner is a database trigger (activation, operator, scanner). A
	// trigger cannot emit a Go metric, so the maintainer counts them when it
	// consumes them. fact_upsert and fact_retract are counted by their writers
	// at capture time, and audit by the audit, so none of them is here.
	TriggerCaptured WinnersTriggerCaptured
}

// WinnersTriggerCaptured is the dirty-log rows one keyed tick consumed, by the
// capture sources a database trigger owns (#9805). It is a plain struct so a
// resweep result stays comparable.
type WinnersTriggerCaptured struct {
	Activation int64
	Operator   int64
	Scanner    int64
}

// SupplyChainImpactWinnersLeaseManager is the single-owner lease seam, satisfied
// by the existing shared-intent partition lease store.
type SupplyChainImpactWinnersLeaseManager interface {
	ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error)
	ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error
}

// SupplyChainImpactWinnersMaintainer runs the periodic atomic resweep.
type SupplyChainImpactWinnersMaintainer struct {
	Rebuilder    SupplyChainImpactWinnersRebuilder
	LeaseManager SupplyChainImpactWinnersLeaseManager
	// WatermarkReader is optional. When set, every tick feeds the committed
	// watermark to the age gauge, whatever the tick then does, so the gauge keeps
	// climbing on an instance that holds no lease or has a failing resweep.
	WatermarkReader SupplyChainImpactWinnersWatermarkReader
	// Now defaults to time.Now (UTC stamped at use). Injected in tests.
	Now func() time.Time
	// Interval is the resweep cadence; defaults to 30s.
	Interval time.Duration
	// LeaseOwner identifies this instance; defaults to a stable owner name.
	LeaseOwner string
	// LeaseTTL bounds a lost-owner takeover; defaults to 60s.
	LeaseTTL time.Duration
	Logger   *slog.Logger
	// Instruments and Tracer are optional. Nil records nothing and starts no
	// span, byte-identical to the behavior before #9774.
	Instruments *telemetry.Instruments
	Tracer      trace.Tracer
	// KeyedRebuilder, Auditor, AuditInterval and DirtyLogReader enable the keyed
	// resweep of #9805. With KeyedRebuilder nil the maintainer is the full-resweep
	// maintainer of #9774. Run then runs one full resweep at startup and keyed
	// ticks after it. The audit runs only when Auditor is set and AuditInterval
	// is positive. DirtyLogReader feeds the dirty-log gauges every tick.
	KeyedRebuilder SupplyChainImpactWinnersKeyedRebuilder
	Auditor        SupplyChainImpactWinnersAuditor
	AuditInterval  time.Duration
	DirtyLogReader SupplyChainImpactWinnersDirtyLogReader
}

func (m SupplyChainImpactWinnersMaintainer) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m SupplyChainImpactWinnersMaintainer) interval() time.Duration {
	if m.Interval > 0 {
		return m.Interval
	}
	return defaultSupplyChainImpactWinnersInterval
}

func (m SupplyChainImpactWinnersMaintainer) leaseOwner() string {
	if m.LeaseOwner != "" {
		return m.LeaseOwner
	}
	return defaultSupplyChainImpactWinnersLeaseOwner
}

func (m SupplyChainImpactWinnersMaintainer) leaseTTL() time.Duration {
	if m.LeaseTTL > 0 {
		return m.LeaseTTL
	}
	return defaultSupplyChainImpactWinnersLeaseTTL
}

func (m SupplyChainImpactWinnersMaintainer) validate() error {
	if m.Rebuilder == nil {
		return fmt.Errorf("supply chain impact winners maintainer requires a rebuilder")
	}
	if m.LeaseManager == nil {
		return fmt.Errorf("supply chain impact winners maintainer requires a lease manager")
	}
	return nil
}

// RunOnce claims the single-owner lease and, if acquired, runs one atomic
// full resweep. It returns rebuilt=true only when this instance held the lease
// and the resweep committed; a resweep skipped because another is in flight
// returns rebuilt=false and no error. The lease is always released before
// returning so a crashed/slow instance does not block takeover beyond the TTL.
func (m SupplyChainImpactWinnersMaintainer) RunOnce(ctx context.Context) (rebuilt bool, err error) {
	return m.runTick(ctx, false)
}

// Run resweeps once immediately (startup backfill/reconcile) and then on the
// configured cadence until ctx is cancelled. With a KeyedRebuilder, the startup
// reconcile is the one full resweep and every later tick is keyed; the audit
// runs on its own cadence beside them. Errors are logged and retried with
// exponential backoff capped at maxSupplyChainImpactWinnersBackoff; the loop
// never exits on a transient resweep error so the read model keeps converging.
func (m SupplyChainImpactWinnersMaintainer) Run(ctx context.Context) error {
	if err := m.validate(); err != nil {
		return err
	}
	var fullDone atomic.Bool
	var audits sync.WaitGroup
	if m.Auditor != nil && m.AuditInterval > 0 {
		audits.Add(1)
		go func() {
			defer audits.Done()
			m.runAuditLoop(ctx, &fullDone)
		}()
	}
	defer audits.Wait()
	backoff := m.interval()
	for {
		if ctx.Err() != nil {
			return nil
		}
		var err error
		if m.KeyedRebuilder != nil && fullDone.Load() {
			_, err = m.runKeyedTick(ctx)
		} else {
			var rebuilt bool
			rebuilt, err = m.RunOnce(ctx)
			if rebuilt && m.KeyedRebuilder != nil {
				fullDone.Store(true)
			}
		}
		switch {
		case err != nil:
			if m.Logger != nil {
				m.Logger.Error("supply chain impact winners resweep failed", "error", err)
			}
			backoff *= 2
			if backoff > maxSupplyChainImpactWinnersBackoff {
				backoff = maxSupplyChainImpactWinnersBackoff
			}
		default:
			backoff = m.interval()
		}
		if err := m.wait(ctx, backoff); err != nil {
			return nil
		}
	}
}

// wait sleeps for d or returns early when ctx is cancelled.
func (m SupplyChainImpactWinnersMaintainer) wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// The winners maintainer's operator signals (#9774): one span, one duration
// sample, four row-action counts and one info log line per committed resweep, a
// skip count and log line for a resweep that found another in flight, and a
// watermark age the gauge computes at scrape time. None of them carries a
// canonical key, scope, repository or CVE.

// sampleWatermark feeds the committed watermark to the age gauge. It runs before
// the lease claim on every tick, so a statement that never returns still leaves
// the gauge a watermark to age. A failed read keeps the last value, which keeps
// aging: this read is what proves the watermark moved, not what proves it stuck.
func (m SupplyChainImpactWinnersMaintainer) sampleWatermark(ctx context.Context) {
	if m.WatermarkReader == nil || m.Instruments == nil {
		return
	}
	at, ok, err := m.WatermarkReader.Watermark(ctx)
	if err != nil {
		if m.Logger != nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners watermark read failed", "error", err)
		}
		return
	}
	if ok {
		m.Instruments.RecordSupplyChainImpactWinnersWatermark(at)
	}
}

// emitResweepSkipped records a resweep that found another in flight. Skips mean
// a resweep outlasted the lease TTL, so a steady rate is a headroom signal.
func (m SupplyChainImpactWinnersMaintainer) emitResweepSkipped(ctx context.Context, span trace.Span) {
	span.SetAttributes(attribute.Bool("skipped", true))
	if in := m.Instruments; in != nil && in.SupplyChainImpactWinnersResweepSkipped != nil {
		in.SupplyChainImpactWinnersResweepSkipped.Add(ctx, 1)
	}
	if m.Logger != nil {
		m.Logger.InfoContext(ctx, "supply chain impact winners resweep skipped: another resweep is in flight")
	}
}

// startResweepSpan opens the resweep span. A nil tracer returns a no-op span
// so ending it never ends the caller's span.
func (m SupplyChainImpactWinnersMaintainer) startResweepSpan(ctx context.Context) (context.Context, trace.Span) {
	if m.Tracer == nil {
		return ctx, trace.SpanFromContext(context.Background())
	}
	return m.Tracer.Start(ctx, telemetry.SpanSupplyChainImpactWinnersResweep)
}

// emitResweep records every signal of one committed resweep. stamp is the
// watermark the resweep wrote.
func (m SupplyChainImpactWinnersMaintainer) emitResweep(
	ctx context.Context,
	span trace.Span,
	stamp time.Time,
	r SupplyChainImpactWinnersResweep,
	elapsed time.Duration,
) {
	path := r.Path
	if path == "" {
		path = telemetry.SupplyChainImpactWinnersPathFull
	}
	span.SetAttributes(
		attribute.String(telemetry.MetricDimensionPath, path),
		attribute.Int64("keys", r.Keys),
		attribute.Int64("inserted", r.Inserted),
		attribute.Int64("updated", r.Updated),
		attribute.Int64("deleted", r.Deleted),
		attribute.Int64("unchanged", r.Unchanged),
	)
	// watermarkAge is -1 when the table had never been swept: there was no
	// watermark to age.
	watermarkAge := -1.0
	if !r.PreviousWatermark.IsZero() {
		watermarkAge = stamp.Sub(r.PreviousWatermark).Seconds()
	}
	if in := m.Instruments; in != nil {
		if in.SupplyChainImpactWinnersResweepDuration != nil {
			in.SupplyChainImpactWinnersResweepDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(telemetry.AttrPath(path)))
		}
		if in.SupplyChainImpactWinnersRows != nil {
			for action, n := range map[string]int64{
				telemetry.SupplyChainImpactWinnersActionInserted:  r.Inserted,
				telemetry.SupplyChainImpactWinnersActionUpdated:   r.Updated,
				telemetry.SupplyChainImpactWinnersActionDeleted:   r.Deleted,
				telemetry.SupplyChainImpactWinnersActionUnchanged: r.Unchanged,
			} {
				if n > 0 {
					in.SupplyChainImpactWinnersRows.Add(ctx, n, metric.WithAttributes(telemetry.AttrAction(action)))
				}
			}
		}
		m.recordKeyedSignals(ctx, r)
		in.RecordSupplyChainImpactWinnersWatermark(stamp)
	}
	if m.Logger == nil {
		return
	}
	m.Logger.InfoContext(ctx, "supply chain impact winners resweep committed",
		slog.String("path", path),
		slog.Int64("identifiers", r.Identifiers),
		slog.Int64("keys", r.Keys),
		slog.Int64("inserted", r.Inserted),
		slog.Int64("updated", r.Updated),
		slog.Int64("deleted", r.Deleted),
		slog.Int64("unchanged", r.Unchanged),
		slog.Float64("duration_seconds", elapsed.Seconds()),
		slog.Float64("previous_watermark_age_seconds", watermarkAge),
	)
}
