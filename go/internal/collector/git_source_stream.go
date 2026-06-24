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
			s.Logger.DebugContext(
				ctx, "collector stream: no repositories discovered",
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
		streamCtx, streamSpan = s.Tracer.Start(
			ctx, telemetry.SpanCollectorStream,
			trace.WithAttributes(
				attribute.String("component", s.componentName()),
				attribute.Int("repository_count", len(resolved)),
				attribute.Int("snapshot_workers", workers),
			),
		)
	}

	streamStart := time.Now()
	if s.Logger != nil {
		s.Logger.InfoContext(
			streamCtx, "collector stream started",
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
				s.Instruments.LargeRepoClassifications.Add(
					workerCtx, 1,
					metric.WithAttributes(attribute.String(telemetry.MetricDimensionRepoSizeTier, tier)),
				)
			}

			ch := smallCh
			if large {
				ch = largeCh
				if s.Logger != nil {
					s.Logger.InfoContext(
						workerCtx, "large repository queued",
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

	// Snapshot workers with two-lane select: prefer small repos so they
	// always flow at full parallelism even when large repos hold the
	// semaphore. Large repos are processed when no small work is available.
	//
	// Key design: the large-repo semaphore is acquired inside the select
	// statement, NOT inside processRepo. This guarantees a worker never
	// blocks waiting for the semaphore while small repos are available.
	// When the semaphore is full, the largeSem case simply doesn't fire
	// and the worker falls through to smallCh or blocks.
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	var completed atomic.Int64

	// drainLarge is a helper for drain loops where the small channel is
	// closed and only large repos remain. Each repo acquires the semaphore
	// before processing and releases it via the afterSnapshot callback.
	drainLarge := func(workerID int) {
		for repo := range largeCh {
			if workerCtx.Err() != nil {
				return
			}
			semWaitStart := time.Now()
			select {
			case largeSem <- struct{}{}:
			case <-workerCtx.Done():
				return
			}
			if s.Instruments != nil {
				s.Instruments.LargeRepoSemaphoreWait.Record(
					workerCtx,
					time.Since(semWaitStart).Seconds(),
				)
			}
			if s.Logger != nil {
				s.Logger.InfoContext(
					workerCtx, "large repo semaphore acquired",
					slog.String("repo_path", repo.RepoPath),
					slog.Int("worker_id", workerID),
					slog.Float64("wait_seconds", time.Since(semWaitStart).Seconds()),
				)
			}
			s.processRepo(workerCtx, repo,
				func() { <-largeSem },
				sourceRunID, observedAt, workerID,
				&errOnce, &firstErr, cancel, &completed)
		}
	}

	for i := 0; i < workers; i++ {
		workerID := i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if workerCtx.Err() != nil {
					return
				}

				// Priority: always try small repos first (non-blocking).
				select {
				case repo, ok := <-smallCh:
					if !ok {
						drainLarge(workerID)
						return
					}
					s.processRepo(workerCtx, repo, nil,
						sourceRunID, observedAt, workerID,
						&errOnce, &firstErr, cancel, &completed)
					continue
				default:
				}

				// No small repo immediately available. Try to acquire the
				// large-repo semaphore alongside checking for small repos.
				// A worker NEVER blocks waiting for the semaphore — if it's
				// full, the largeSem case doesn't fire and the worker handles
				// small repos or waits for either channel.
				select {
				case repo, ok := <-smallCh:
					if !ok {
						drainLarge(workerID)
						return
					}
					s.processRepo(workerCtx, repo, nil,
						sourceRunID, observedAt, workerID,
						&errOnce, &firstErr, cancel, &completed)
				case largeSem <- struct{}{}:
					// Acquired semaphore — pull a large repo.
					semAcquiredAt := time.Now()
					select {
					case repo, ok := <-largeCh:
						if !ok {
							<-largeSem
							// Large channel closed, drain remaining small repos.
							for repo := range smallCh {
								if workerCtx.Err() != nil {
									return
								}
								s.processRepo(workerCtx, repo, nil,
									sourceRunID, observedAt, workerID,
									&errOnce, &firstErr, cancel, &completed)
							}
							return
						}
						if s.Instruments != nil {
							s.Instruments.LargeRepoSemaphoreWait.Record(workerCtx, 0)
						}
						if s.Logger != nil {
							s.Logger.InfoContext(
								workerCtx, "large repo semaphore acquired",
								slog.String("repo_path", repo.RepoPath),
								slog.Int("worker_id", workerID),
								slog.Float64("wait_seconds", 0),
							)
						}
						// Process large repo; afterSnapshot releases semaphore
						// so it's freed before the (potentially slow) stream send.
						s.processRepo(workerCtx, repo,
							func() {
								<-largeSem
								if s.Logger != nil {
									s.Logger.InfoContext(
										workerCtx, "large repo semaphore released",
										slog.Int("worker_id", workerID),
										slog.Float64("held_seconds", time.Since(semAcquiredAt).Seconds()),
									)
								}
							},
							sourceRunID, observedAt, workerID,
							&errOnce, &firstErr, cancel, &completed)
					case repo, ok := <-smallCh:
						// Got a small repo while waiting for large — release sem.
						<-largeSem
						if !ok {
							drainLarge(workerID)
							return
						}
						s.processRepo(workerCtx, repo, nil,
							sourceRunID, observedAt, workerID,
							&errOnce, &firstErr, cancel, &completed)
					case <-workerCtx.Done():
						<-largeSem
						return
					}
				case <-workerCtx.Done():
					return
				}
			}
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
			s.Instruments.CollectorObserveDuration.Record(
				ctx, streamDuration,
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
				logAttrs = append(
					logAttrs,
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
