// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExecuteEntityPhaseGroupStreaming dispatches canonical entity-phase chunks to
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
// Errors from any worker surface through `errCh` and cancel `poolCtx`, which
// only gates admission of new work: the producer's `pushChunk` returns early
// when `poolCtx` is done (so a failing run does not push more chunks into a
// draining pool), and idle workers stop pulling from `jobs` once it is
// canceled. `poolCtx` is never passed to `ge.ExecuteGroup` — each in-flight
// chunk executes against `ctx` (the caller's context) directly, so one
// chunk's write-timeout error cannot cancel a sibling chunk's already-running
// canonical write (#4464 Bug 1: a single slow/timed-out MERGE must not abort
// concurrent siblings). The function waits for workers to drain on every exit
// path so callers do not leak goroutines or see partial per-label stats.
func (e PhaseGroupExecutor) ExecuteEntityPhaseGroupStreaming(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	phase := statementPhase(stmts)
	labelStats := make(map[string]*entityPhaseLabelStats)
	pool := newEntityPhaseStreamingPool(ctx, ge, e, phase, labelStats)
	defer pool.cancel()

	producerErr := dispatchEntityPhaseStatements(ctx, stmts, pool)
	pool.close()
	if producerErr != nil {
		return pool.preferError(producerErr)
	}
	if err := pool.preferError(nil); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stream entity phase parent context: %w", err)
	}
	logEntityPhaseLabelSummaries(labelStats, true)
	return nil
}

func dispatchEntityPhaseStatements(
	ctx context.Context,
	stmts []sourcecypher.Statement,
	pool *entityPhaseStreamingPool,
) error {
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
		limit := pool.executor.PhaseGroupStatementLimit(grouped)
		label := groupedLabel
		for start := 0; start < len(grouped); start += limit {
			end := start + limit
			if end > len(grouped) {
				end = len(grouped)
			}
			chunk := append([]sourcecypher.Statement(nil), grouped[start:end]...)
			if err := pool.push(chunk, label); err != nil {
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
				return err
			}
			if err := pool.drain(); err != nil {
				return err
			}
			chunks := sourcecypher.ChunkPositiveStringSliceRetractStatement(
				stmt,
				sourcecypher.DefaultPositiveRetractStringSliceBatchSize,
			)
			for chunkIndex, chunk := range chunks {
				statementStart := time.Now()
				statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{chunk})
				if err := pool.executor.Inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
					return fmt.Errorf(
						"phase-group retract statement %d/%d part %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
						i+1,
						len(stmts),
						chunkIndex+1,
						len(chunks),
						pool.phase,
						time.Since(statementStart),
						statementSummary,
						err,
					)
				}
			}
			continue
		}
		if statementPhaseGroupMode(stmt) == sourcecypher.PhaseGroupModeExecuteOnly {
			if err := flushGrouped(); err != nil {
				return err
			}
			if err := pool.drain(); err != nil {
				return err
			}
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := pool.executor.Inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
				return fmt.Errorf(
					"phase-group singleton statement %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
					i+1, len(stmts), pool.phase, time.Since(statementStart), statementSummary, err,
				)
			}
			slog.Info(
				"nornicdb phase-group singleton completed",
				"statement_index", i+1,
				"statement_count", len(stmts),
				"phase", pool.phase,
				"duration_s", time.Since(statementStart).Seconds(),
				"first_statement", statementSummary,
			)
			pool.recordSingleton(stmt, time.Since(statementStart))
			continue
		}
		stmtLabel := entityStatementLabel(stmt)
		if groupedLabel != "" && stmtLabel != groupedLabel {
			completedLabel := groupedLabel
			if err := flushGrouped(); err != nil {
				return err
			}
			if err := pool.drain(); err != nil {
				return err
			}
			pool.completeLabel(completedLabel)
			groupedLabel = ""
		}
		grouped = append(grouped, stmt)
		if groupedLabel == "" {
			groupedLabel = stmtLabel
		}
		// Push as soon as the buffer reaches one chunk's worth so the pool
		// streams without waiting for the legacy `limit * concurrency`
		// batch trigger.
		if len(grouped) >= pool.executor.PhaseGroupStatementLimit(grouped) {
			chunk := append([]sourcecypher.Statement(nil), grouped...)
			label := groupedLabel
			grouped = grouped[:0]
			if err := pool.push(chunk, label); err != nil {
				return err
			}
		}
	}

	if err := flushGrouped(); err != nil {
		return err
	}
	return nil
}
