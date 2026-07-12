// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// drainCollector runs the collector source until no more work is available.
// Each cycle is wrapped in a collector.observe span with metric and log output
// so operators can trace collection throughput during bootstrap.
//
// Commit lanes (#5130): the source is polled by a single dispatcher (the
// GitSource stream contract is single-consumer), while commits run on
// commitLanes concurrent workers. The accepted 896-repo run measured the
// commit chain busy 99.66% of the collection span with upsert_facts strictly
// serialized (max_concurrency=1, 921.1s — #5122); scopes are independent
// conflict domains (per-scope transactions, per-repo shared-lock keys, a
// concurrency-safe catalog cache), and the scope-partitioned lane shim on
// the real fact distribution measured 1.95x at 2 lanes with a 2.22x plateau
// at 4. The work channel is unbuffered so backpressure semantics are
// unchanged: the dispatcher hands a generation to the next free lane or
// blocks, exactly as the serial loop blocked on its own commit.
//
// Per-repo instrumentation added by #3678:
//   - eshu_dp_content_entity_emitted_total (source_file_kind, collector_kind):
//     incremented per entity by bounded file kind so lockfile/config explosions
//     are visible from the metrics port without manual SQL.
//   - Periodic progress log every bootstrapProgressInterval repos (repos done,
//     elapsed, facts emitted) so a 70-min run produces visible progress in logs.
//   - Per-repo content_entity breakdown in the "bootstrap scope collected" log
//     line (content_entity_count, entity_by_source_file_kind).
func drainCollector(
	ctx context.Context,
	source collector.Source,
	committer collector.Committer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	commitLanes int,
	advisorySinks ...discoveryAdvisorySink,
) error {
	if commitLanes < 1 {
		commitLanes = 1
	}
	state := &collectorDrainState{
		committer:       committer,
		instruments:     instruments,
		logger:          logger,
		advisorySink:    firstDiscoveryAdvisorySink(advisorySinks),
		collectionStart: time.Now(),
	}

	// admissionCtx gates the SOURCE and NEW admission only. A lane commit
	// failure cancels it to stop pulling and dispatching further generations
	// (and to unwind the snapshot pipeline's blocking sends), but commits
	// already admitted to a lane run under the PARENT context so they finish
	// atomically — only parent cancellation cancels admitted work (#5135
	// review, finding 1).
	admissionCtx, stopAdmission := context.WithCancel(ctx)
	defer stopAdmission()

	type collectCycle struct {
		collected  collector.CollectedGeneration
		cycleCtx   context.Context
		span       trace.Span
		cycleStart time.Time
	}
	// admissionKey serializes conflicting generations: two generations
	// sharing a ScopeID or PartitionKey must never commit concurrently
	// (#5135 review, finding 3). Bootstrap emits unique scopes, so this is a
	// guard for future sources, not a hot path.
	type admissionKey struct {
		scopeID      string
		partitionKey string
	}
	work := make(chan collectCycle)
	laneDone := make(chan admissionKey, commitLanes)
	var wg sync.WaitGroup
	for lane := 0; lane < commitLanes; lane++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cycle := range work {
				key := admissionKey{
					scopeID:      cycle.collected.Scope.ScopeID,
					partitionKey: cycle.collected.Scope.PartitionKey,
				}
				// A cycle handed off in the same instant a sibling failure
				// stopped admission is rejected, not committed: drain its
				// fact stream so the producer is not stranded, and release
				// its key. This closes the send-versus-cancel race where one
				// extra cycle could slip past the admission stop.
				if state.firstError() != nil {
					if cycle.collected.Facts != nil {
						for range cycle.collected.Facts {
						}
					}
					if cycle.span != nil {
						cycle.span.End()
					}
					laneDone <- key
					continue
				}
				err := state.commitCollectedGeneration(
					cycle.cycleCtx, cycle.collected, cycle.span, cycle.cycleStart,
				)
				if err != nil {
					// Record the failure and stop admission BEFORE releasing
					// the key, so the dispatcher observes the stop no later
					// than the release.
					state.recordError(err)
					stopAdmission()
				}
				laneDone <- key
			}
		}()
	}

	activeScopes := make(map[string]struct{}, commitLanes)
	activePartitions := make(map[string]struct{}, commitLanes)
	releaseKey := func(key admissionKey) {
		delete(activeScopes, key.scopeID)
		if key.partitionKey != "" {
			delete(activePartitions, key.partitionKey)
		}
	}
	drainReleasedKeys := func() {
		for {
			select {
			case key := <-laneDone:
				releaseKey(key)
			default:
				return
			}
		}
	}
	keyBusy := func(key admissionKey) bool {
		if _, busy := activeScopes[key.scopeID]; busy {
			return true
		}
		if key.partitionKey != "" {
			if _, busy := activePartitions[key.partitionKey]; busy {
				return true
			}
		}
		return false
	}

	// Dispatcher: single consumer of the source stream. It stops on source
	// exhaustion, source error, or the first lane failure (admission stop).
	var dispatchErr error
	for dispatchErr == nil && admissionCtx.Err() == nil {
		cycleStart := time.Now()

		var span trace.Span
		// The observe span parents on admissionCtx so the SOURCE read is
		// admission-gated; the span context handed to the lane is re-rooted
		// on the parent below so an admitted commit is not canceled by an
		// admission stop.
		sourceCtx := admissionCtx
		if tracer != nil {
			sourceCtx, span = tracer.Start(
				admissionCtx, telemetry.SpanCollectorObserve,
				trace.WithAttributes(attribute.String("component", "bootstrap-index")),
			)
		}

		collected, ok, err := source.Next(sourceCtx)
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			// An admission stop after a commit failure is that failure's
			// consequence, not an independent source error — the lane error
			// is joined below. Parent-driven cancellation must stay a
			// visible collector error, never a silent partial-collection
			// success.
			if admissionCtx.Err() != nil && errors.Is(err, admissionCtx.Err()) && state.firstError() != nil {
				break
			}
			dispatchErr = fmt.Errorf("bootstrap collector: %w", err)
			break
		}
		if !ok {
			if span != nil {
				span.End()
			}
			break
		}

		if instruments != nil {
			instruments.FactsEmitted.Add(sourceCtx, int64(collected.FactCount()), metric.WithAttributes(
				telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}

		// The admitted commit runs under the parent context: re-root the
		// span context so a later admission stop cannot cancel it (#5135
		// review, finding 1). Trace correlation is preserved via the span.
		cycleCtx := ctx
		if span != nil {
			cycleCtx = trace.ContextWithSpan(ctx, span)
		}

		key := admissionKey{scopeID: collected.Scope.ScopeID, partitionKey: collected.Scope.PartitionKey}
		drainReleasedKeys()
		admitted := false
		for {
			for keyBusy(key) && admissionCtx.Err() == nil {
				// Wait for a lane to release a key, then re-check.
				select {
				case done := <-laneDone:
					releaseKey(done)
				case <-admissionCtx.Done():
				}
			}
			if admissionCtx.Err() != nil {
				break
			}
			select {
			case work <- collectCycle{collected: collected, cycleCtx: cycleCtx, span: span, cycleStart: cycleStart}:
				activeScopes[key.scopeID] = struct{}{}
				if key.partitionKey != "" {
					activePartitions[key.partitionKey] = struct{}{}
				}
				admitted = true
			case done := <-laneDone:
				// A lane freed up (and possibly failed) while we were
				// blocked on hand-off; release and retry the admission
				// check so a post-failure cycle is never dispatched.
				releaseKey(done)
				continue
			case <-admissionCtx.Done():
			}
			break
		}
		if !admitted {
			// Received from the source but never dispatched: drain the fact
			// stream to exhaustion so its blocking-send producer goroutine
			// is not stranded (#5135 review, finding 2).
			if collected.Facts != nil {
				for range collected.Facts {
				}
			}
			if span != nil {
				span.End()
			}
		}
	}
	close(work)
	wg.Wait()
	drainReleasedKeys()

	if err := errors.Join(dispatchErr, state.firstError()); err != nil {
		return err
	}
	// Parent-driven cancellation (deadline or signal wiring) with no lane
	// failure must stay a visible collector error: the collection is partial
	// and must never be reported as complete. Only ctx — not laneCtx, which a
	// failed lane cancels itself — distinguishes the parent case.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bootstrap collector: %w", err)
	}

	if logger != nil {
		logger.InfoContext(
			ctx, "bootstrap collection complete",
			slog.Int("scopes_collected", int(state.total.Load())),
			slog.Int64("total_facts_emitted", state.totalFacts.Load()),
			slog.Int64("total_entities_emitted", state.totalEntities.Load()),
			slog.Float64("collection_duration_seconds", time.Since(state.collectionStart).Seconds()),
			slog.Int("commit_lanes", commitLanes),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		)
	}
	return nil
}

// effectiveCommitLanes bounds the requested commit-lane count by the measured
// throughput plateau AND by shared Postgres pool headroom (#5135 review,
// finding 4). The #5122 lane shim measured the plateau at 4 lanes (8 was
// flat), so more than 4 is never a throughput win; and because every lane
// holds an open transaction on the pool it shares with the concurrent
// projector, max(2, projectionWorkers+1) connections stay reserved for
// projection and maintenance before commit concurrency is granted. Never
// returns less than one lane.
func effectiveCommitLanes(requested, maxOpenConns, projectionWorkers int) int {
	lanes := requested
	if lanes < 1 {
		lanes = 1
	}
	if lanes > defaultCommitLanes {
		lanes = defaultCommitLanes
	}
	reserved := projectionWorkers + 1
	if reserved < 2 {
		reserved = 2
	}
	budget := maxOpenConns - reserved
	if budget < 1 {
		budget = 1
	}
	if lanes > budget {
		lanes = budget
	}
	return lanes
}

// postgresMaxOpenConns reads ESHU_POSTGRES_MAX_OPEN_CONNS with the platform
// default (30, see internal/envregistry), for the commit-lane pool budget.
func postgresMaxOpenConns(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_POSTGRES_MAX_OPEN_CONNS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 30
}
