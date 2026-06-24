// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// startStream performs synchronous repo discovery, then launches background
// snapshot workers that feed generations into s.stream. The channel buffer
// equals the worker count, providing natural backpressure: workers block on
// send when the consumer hasn't committed the previous generation yet.
//
// Telemetry:
//   - Parent span: collector.stream (covers entire stream lifecycle)
//   - Child spans: fact.emit (one per repository, from snapshotOneRepository)
//   - Metrics: RepoSnapshotDuration, ReposSnapshotted, FactEmitDuration, FactsEmitted
//   - Logging: stream start (repos discovered, workers), stream end (completed, failed, duration)
func (s *GitSource) startStream(ctx context.Context) error {
	// Phase 1: Discovery (synchronous, fast)
	batch, err := s.discoverRepositories(ctx)
	if err != nil {
		return err
	}
	if len(batch.Repositories) == 0 {
		if s.Logger != nil {
			s.Logger.DebugContext(ctx, "collector stream: no repositories discovered",
				slog.String("collector_kind", "git"),
				slog.String("component", s.componentName()),
				telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			)
		}
		s.stream = make(chan CollectedGeneration)
		close(s.stream)
		return nil
	}

	// Phase 2: Resolve paths, order largest-first, and compute stable source
	// run ID. resolvedCounts is aligned 1:1 with resolved so the small/large
	// lane classification below reuses the file count walked here instead of
	// re-walking each repository tree.
	resolved, resolvedCounts, sourceRunID, err := s.resolveRepositories(batch)
	if err != nil {
		return err
	}

	// Phase 3: Launch background snapshot workers
	workers := s.SnapshotWorkers
	if workers <= 0 {
		workers = 8
	}

	// Size-tiered scheduling: large repos acquire a semaphore before
	// snapshotting so at most N large repos parse concurrently. Small repos
	// bypass the semaphore entirely and use all available workers.
	largeThreshold := s.largeRepoThreshold()
	largeMaxConcurrent := s.LargeRepoMaxConcurrent
	if largeMaxConcurrent <= 0 {
		largeMaxConcurrent = 2
	}
	largeSem := make(chan struct{}, largeMaxConcurrent)

	// Stream buffer: sized to the worker count by default so completed
	// small-repo snapshots don't block behind slow large-repo commits.
	// Each buffered generation holds metadata + a fact channel reference;
	// file bodies are re-read from disk via two-phase streaming, so the
	// per-slot memory cost is negligible.
	streamBuf := s.StreamBuffer
	if streamBuf <= 0 {
		streamBuf = workers
	}
	s.stream = make(chan CollectedGeneration, streamBuf)
	s.streamErr = nil

	// Start the parent stream span — kept open until coordinator closes it
	var streamSpan trace.Span
	streamCtx := ctx
	if s.Tracer != nil {
		streamCtx, streamSpan = s.Tracer.Start(ctx, telemetry.SpanCollectorStream,
			trace.WithAttributes(
				attribute.String("component", s.componentName()),
				attribute.Int("repository_count", len(resolved)),
				attribute.Int("snapshot_workers", workers),
			),
		)
	}

	streamStart := time.Now()
	if s.Logger != nil {
		s.Logger.InfoContext(streamCtx, "collector stream started",
			slog.String("collector_kind", "git"),
			slog.String("component", s.componentName()),
			slog.Int("repository_count", len(resolved)),
			slog.Int("snapshot_workers", workers),
			slog.Int("stream_buffer", streamBuf),
			slog.Int("large_repo_threshold", largeThreshold),
			slog.Int("large_repo_max_concurrent", largeMaxConcurrent),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		)
	}

	workerCtx, cancel := context.WithCancel(streamCtx)
	observedAt := batch.ObservedAt.UTC()

	// Two-lane channels: discovery classifies repos up front and routes
	// them to separate channels so workers can prefer small repos. This
	// prevents head-of-line blocking when large repos cluster together
	// (the "convoy" problem observed in production).
	//
	// Channels are buffered at len(resolved) so the discovery goroutine
	// sends all repos immediately without blocking on either lane. This
	// prevents a large-repo cluster from stalling discovery and starving
	// smallCh of repos that come later in alphabetical order. The memory
	// cost is ~500 bytes per SelectedRepository — negligible at 878 repos.
	smallCh := make(chan SelectedRepository, len(resolved))
	largeCh := make(chan SelectedRepository, len(resolved))

	// Discovery goroutine: classify once per repo and route to the
	// appropriate lane. Classification reuses the file count already walked in
	// resolveRepositories (aligned 1:1 with resolved), so no early-bail pre-scan
	// re-walks the tree here.
	go func() {
		defer close(smallCh)
		defer close(largeCh)
		for i, repo := range resolved {
			// Reuse the file count walked in resolveRepositories (aligned 1:1
			// with resolved) so classification does not re-walk the tree.
			large := resolvedCounts[i] > largeThreshold

			// Record size-tier classification.
			if s.Instruments != nil {
				tier := "small"
				if large {
					tier = "large"
				}
				s.Instruments.LargeRepoClassifications.Add(workerCtx, 1,
					metric.WithAttributes(attribute.String(telemetry.MetricDimensionRepoSizeTier, tier)),
				)
			}

			ch := smallCh
			if large {
				ch = largeCh
				if s.Logger != nil {
					s.Logger.InfoContext(workerCtx, "large repository queued",
						slog.String("repo_path", repo.RepoPath),
					)
				}
			}

			select {
			case ch <- repo:
			case <-workerCtx.Done():
				return
			}
		}
	}()

	// Snapshot workers run in two roles. The first min(largeMaxConcurrent,
	// workers) workers PREFER the large lane so up to that many giant repos
	// always START in the first scheduling window, regardless of how the
	// classifier front-loads the small lane. The remaining workers prefer the
	// small lane so small repos still flow at full parallelism.
	//
	// Why a dedicated large lane: largest-first ordering only guarantees
	// enqueue order. If every worker prefers small work, a classifier that
	// fills the small lane before workers run keeps each worker taking smalls
	// while the giants sit in the large lane until the small lane drains, so
	// the giant-repo overlap was scheduler-timing-dependent. Dedicating up to
	// largeMaxConcurrent workers to the large lane makes the early giant start
	// deterministic while the semaphore still bounds concurrent giant parses.
	//
	// Key design: the large-repo semaphore is acquired inside the select
	// statement, NOT inside processRepo. Every acquire (`largeSem <- struct{}{}`)
	// has exactly one matching release (`<-largeSem`) on every path: a processed
	// giant releases via the afterSnapshot callback in processLargeRepo; a
	// no-large-ready, large-closed, or ctx-cancelled path releases inline before
	// returning.
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	var completed atomic.Int64

	sched := &snapshotScheduler{
		source:      s,
		smallCh:     smallCh,
		largeCh:     largeCh,
		largeSem:    largeSem,
		workerCtx:   workerCtx,
		cancel:      cancel,
		sourceRunID: sourceRunID,
		observedAt:  observedAt,
		errOnce:     &errOnce,
		firstErr:    &firstErr,
		completed:   &completed,
	}

	largePreferring := largeMaxConcurrent
	if largePreferring > workers {
		largePreferring = workers
	}
	// Reserve at least one small-preferring worker when workers > 1 so small
	// repos are not starved until largeCh closes. When workers == 1 the lone
	// worker is small-preferring and still opportunistically drains large repos
	// via the runSmallPreferring select (it can win the semaphore when no small
	// repo is immediately available).
	if largePreferring >= workers && workers > 1 {
		largePreferring = workers - 1
	}

	for i := 0; i < workers; i++ {
		workerID := i + 1
		// The first largePreferring workers prefer the large lane so up to
		// LargeRepoMaxConcurrent giant repos start in the first scheduling window
		// regardless of how the classifier front-loads the small lane (issue
		// #3839); the rest prefer the small lane to keep small repos flowing.
		// largePreferring is always < workers (enforced above), so at least one
		// worker here takes the small-preferring path.
		preferLarge := i < largePreferring
		wg.Add(1)
		go func() {
			defer wg.Done()
			if preferLarge {
				sched.runLargePreferring(workerID)
				return
			}
			sched.runSmallPreferring(workerID)
		}()
	}

	// Coordinator: wait for all workers, record telemetry, close channel.
	// The channel close happens-before any receive that returns ok==false,
	// so s.streamErr is safely visible to Next() without additional sync.
	go func() {
		wg.Wait()
		cancel()
		s.streamErr = firstErr

		streamDuration := time.Since(streamStart).Seconds()
		completedCount := completed.Load()

		// Record stream-level metrics
		if s.Instruments != nil {
			s.Instruments.CollectorObserveDuration.Record(ctx, streamDuration,
				metric.WithAttributes(
					telemetry.AttrCollectorKind("git"),
					attribute.String("component", s.componentName()),
				),
			)
		}

		// Close stream span
		if streamSpan != nil {
			streamSpan.SetAttributes(
				attribute.Int64("repos_completed", completedCount),
				attribute.Int("repos_total", len(resolved)),
				attribute.Float64("duration_seconds", streamDuration),
			)
			if firstErr != nil {
				streamSpan.RecordError(firstErr)
			}
			streamSpan.End()
		}

		// Log stream completion
		if s.Logger != nil {
			logAttrs := []any{
				slog.String("collector_kind", "git"),
				slog.String("component", s.componentName()),
				slog.Int64("repos_completed", completedCount),
				slog.Int("repos_total", len(resolved)),
				slog.Int("snapshot_workers", workers),
				slog.Float64("duration_seconds", streamDuration),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			}
			if firstErr != nil {
				logAttrs = append(logAttrs,
					slog.String("error", firstErr.Error()),
					telemetry.FailureClassAttr("stream_snapshot_failure"),
				)
				s.Logger.ErrorContext(ctx, "collector stream failed", logAttrs...)
			} else {
				s.Logger.InfoContext(ctx, "collector stream completed", logAttrs...)
			}
		}

		close(s.stream)
	}()

	return nil
}
