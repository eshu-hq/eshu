package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// executeEntityPhaseGroupStreaming dispatches canonical entity-phase chunks to
// a persistent worker pool that runs for the lifetime of the call. Producer
// threads push a chunk to the pool as soon as `limit` statements of one label
// are buffered; workers pull chunks continuously instead of synchronizing on
// per-flush `wg.Wait()` calls between batches.
//
// The K8s dogfood after #173's `executeGroupedChunksConcurrentlyObserved`
// landed (canonical_write 518s → 218s) showed the Variable label at 208s
// wall-clock for 673 chunks × concurrency 8, against a 95s ideal floor. The
// gap came from the legacy `executeEntityPhaseGroup` flush boundary: every
// `flushGrouped` call buffered `limit * concurrency` statements, dispatched
// them as one wave, and blocked the producer on `wg.Wait()` while the
// slowest chunk in the wave finished. Across 84 such waves on the Variable
// label, producer stalls plus stragglers consumed >100s of wall.
//
// Cross-chunk safety inside an entity label rests on the same invariants
// that `executeGroupedChunksConcurrentlyObserved` documents: canonical
// entity MERGE keys on `Label{entity_id}` are disjoint per
// repo+path+kind+identifier within a Materialization, and the file_path
// MATCH is a read-only anchor. Across labels, the streaming path drains
// before logging `complete=true` so per-label stats stay accurate. Retract
// and singleton statements still serialize behind a drain because they read
// graph state mutated by prior chunks.
//
// Errors from any worker surface through `errCh` and cancel `poolCtx`. The
// producer's `pushChunk` returns early when `poolCtx` is done, so a failing
// run does not push more chunks into a canceled pool. The function waits for
// workers to drain on every exit path so callers do not leak goroutines or
// see partial per-label stats.
func (e nornicDBPhaseGroupExecutor) executeEntityPhaseGroupStreaming(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	phase := statementPhase(stmts)
	labelStats := make(map[string]*entityPhaseLabelStats)

	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()

	type chunkJob struct {
		chunk []sourcecypher.Statement
		label string
	}

	jobs := make(chan chunkJob, e.entityPhaseConcurrency)
	errCh := make(chan error, 1)
	var (
		observerMu sync.Mutex
		inFlight   sync.WaitGroup
		workerWG   sync.WaitGroup
	)
	chunkSeq := int64(0)

	raiseErr := func(err error) {
		select {
		case errCh <- err:
		default:
		}
		poolCancel()
	}

	for w := 0; w < e.entityPhaseConcurrency; w++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for job := range jobs {
				func() {
					defer inFlight.Done()
					if poolCtx.Err() != nil {
						return
					}
					idx := atomic.AddInt64(&chunkSeq, 1)
					chunk := job.chunk
					summary := summarizePhaseGroupChunk(chunk)
					start := time.Now()
					err := ge.ExecuteGroup(poolCtx, sanitizedPhaseGroupChunk(chunk))
					duration := time.Since(start)
					if err != nil {
						raiseErr(fmt.Errorf(
							"phase-group chunk %d (size=%d, duration=%s, first_statement=%q): %w",
							idx, len(chunk), duration, summary, err,
						))
						return
					}
					observerMu.Lock()
					stats := ensureEntityPhaseLabelStats(labelStats, phase, job.label, chunk[0])
					stats.recordChunk(chunk, duration)
					logEntityPhaseLabelSummaryIfDue(stats, false)
					observerMu.Unlock()
					slog.Info(
						"nornicdb phase-group chunk completed",
						"chunk_index", idx,
						"statement_count", len(chunk),
						"duration_s", duration.Seconds(),
						"first_statement", summary,
						"concurrency", e.entityPhaseConcurrency,
						"streaming", true,
					)
				}()
			}
		}()
	}

	closePool := func() {
		close(jobs)
		workerWG.Wait()
	}

	// preferPoolError returns a buffered worker error from errCh in preference
	// to a producer-side cancellation error. When a worker fails, raiseErr
	// puts the wrapped failure in errCh and cancels poolCtx; subsequent
	// pushChunk calls see ctx.Done() and return poolCtx.Err() ("context
	// canceled"), which would otherwise mask the real failure. The producer
	// calls this helper before returning so callers see the original
	// ExecuteGroup error class.
	preferPoolError := func(producerErr error) error {
		select {
		case err := <-errCh:
			return err
		default:
		}
		return producerErr
	}

	pushChunk := func(chunk []sourcecypher.Statement, label string) error {
		inFlight.Add(1)
		select {
		case jobs <- chunkJob{chunk: chunk, label: label}:
			return nil
		case <-poolCtx.Done():
			inFlight.Done()
			return poolCtx.Err()
		}
	}

	// drainInFlight waits for chunks already pushed to complete. Required
	// before retract/singleton statements (which read graph state mutated by
	// prior chunks) and before logging a per-label `complete=true` summary
	// (which must reflect the full label's chunk count and timing). After
	// drainInFlight returns nil, no chunks are in flight and the pool is
	// idle until the next push.
	drainInFlight := func() error {
		inFlight.Wait()
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}

	grouped := make([]sourcecypher.Statement, 0)
	groupedLabel := ""

	// flushGrouped pushes any buffered statements as chunks of size `limit`.
	// The buffer can hold multiple chunks' worth when a label transition or
	// terminal statement triggers an early flush; in steady state the
	// per-chunk push below keeps the buffer at most one chunk in size.
	flushGrouped := func() error {
		if len(grouped) == 0 {
			return nil
		}
		limit := e.phaseGroupStatementLimit(grouped)
		label := groupedLabel
		for start := 0; start < len(grouped); start += limit {
			end := start + limit
			if end > len(grouped) {
				end = len(grouped)
			}
			chunk := append([]sourcecypher.Statement(nil), grouped[start:end]...)
			if err := pushChunk(chunk, label); err != nil {
				grouped = grouped[:0]
				groupedLabel = ""
				return err
			}
		}
		grouped = grouped[:0]
		groupedLabel = ""
		return nil
	}

	for i, stmt := range stmts {
		if stmt.Operation == sourcecypher.OperationCanonicalRetract {
			if err := flushGrouped(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			if err := drainInFlight(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
				closePool()
				return fmt.Errorf(
					"phase-group retract statement %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
					i+1, len(stmts), phase, time.Since(statementStart), statementSummary, err,
				)
			}
			continue
		}
		if statementPhaseGroupMode(stmt) == sourcecypher.PhaseGroupModeExecuteOnly {
			if err := flushGrouped(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			if err := drainInFlight(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
				closePool()
				return fmt.Errorf(
					"phase-group singleton statement %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
					i+1, len(stmts), phase, time.Since(statementStart), statementSummary, err,
				)
			}
			slog.Info(
				"nornicdb phase-group singleton completed",
				"statement_index", i+1,
				"statement_count", len(stmts),
				"phase", phase,
				"duration_s", time.Since(statementStart).Seconds(),
				"first_statement", statementSummary,
			)
			observerMu.Lock()
			stats := ensureEntityPhaseLabelStats(labelStats, phase, entityStatementLabel(stmt), stmt)
			stats.recordSingleton(stmt, time.Since(statementStart))
			logEntityPhaseLabelSummaryIfDue(stats, false)
			observerMu.Unlock()
			continue
		}
		stmtLabel := entityStatementLabel(stmt)
		if len(grouped) > 0 && stmtLabel != groupedLabel {
			completedLabel := groupedLabel
			if err := flushGrouped(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			if err := drainInFlight(); err != nil {
				closePool()
				return preferPoolError(err)
			}
			observerMu.Lock()
			stats := labelStats[completedLabel]
			observerMu.Unlock()
			if stats != nil {
				logEntityPhaseLabelSummaryIfDue(stats, true)
			}
		}
		grouped = append(grouped, stmt)
		if groupedLabel == "" {
			groupedLabel = stmtLabel
		}
		// Push as soon as the buffer reaches one chunk's worth so the pool
		// streams without waiting for the legacy `limit * concurrency`
		// batch trigger.
		if len(grouped) >= e.phaseGroupStatementLimit(grouped) {
			chunk := append([]sourcecypher.Statement(nil), grouped...)
			label := groupedLabel
			grouped = grouped[:0]
			if err := pushChunk(chunk, label); err != nil {
				closePool()
				return preferPoolError(err)
			}
		}
	}

	if err := flushGrouped(); err != nil {
		closePool()
		return preferPoolError(err)
	}
	closePool()

	select {
	case err := <-errCh:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	logEntityPhaseLabelSummaries(labelStats, true)
	return nil
}
