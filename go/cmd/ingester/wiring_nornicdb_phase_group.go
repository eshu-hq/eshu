// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type nornicDBPhaseGroupExecutor struct {
	inner                    sourcecypher.Executor
	maxStatements            int
	directoryMaxStatements   int
	fileMaxStatements        int
	entityMaxStatements      int
	entityLabelMaxStatements map[string]int
	// entityPhaseConcurrency caps how many canonical entity-phase grouped
	// chunks may run in parallel against the inner GroupExecutor. The
	// runtime default is `cpubudget.UsableCPUs()` (cgroup-aware CPU count) clamped to
	// `nornicDBEntityPhaseConcurrencyCap`, so most callers route through
	// the streaming dispatcher in wiring_nornicdb_phase_group_streaming.go.
	// A value of zero or one is an explicit serial override: it pins
	// ExecutePhaseGroup to the legacy per-flush executeEntityPhaseGroup
	// path so callers that need deterministic chunk ordering (or that are
	// debugging a streaming-specific regression) can opt out. Cross-chunk
	// safety inside an entity label rests on disjoint entity_id MERGE keys
	// plus the file_path MATCH-only contract; see
	// executeGroupedChunksConcurrentlyObserved for the full safety
	// argument.
	entityPhaseConcurrency int

	// drainReader drives the bounded drain loop for full-refresh DETACH DELETE
	// statements marked with Drain=true. When nil, Drain statements fall back
	// to a single Execute call (backward-compatible with existing tests that
	// do not wire a reader). In production, rawExecutor (ingesterNeo4jExecutor)
	// is threaded here via retractDrainReader.
	drainReader retractDrainReader

	// retractBatchSize is the LIMIT applied to each drain-loop iteration.
	// Controlled by ESHU_CANONICAL_RETRACT_BATCH.
	retractBatchSize int

	// instruments carries the OTEL metric handles for recording reconciliation
	// drift retraction counters after each drain loop completes. May be nil in
	// tests that do not wire telemetry; RecordReconciliationDriftRetractions
	// is a no-op when instruments is nil.
	instruments *telemetry.Instruments
}

func (e nornicDBPhaseGroupExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.inner == nil {
		return nil
	}
	return e.inner.Execute(ctx, sanitizedStatement(stmt))
}

func (e nornicDBPhaseGroupExecutor) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) == 0 || e.inner == nil {
		return nil
	}
	if ge, ok := e.inner.(sourcecypher.GroupExecutor); ok {
		if allStatementsUseOperation(stmts, sourcecypher.OperationCanonicalRetract) {
			return e.executeSequentialRetractPhase(ctx, stmts)
		}
		if statementPhaseUsesEntityLabelStats(statementPhase(stmts)) {
			if e.entityPhaseConcurrency > 1 {
				return e.executeEntityPhaseGroupStreaming(ctx, ge, stmts)
			}
			return e.executeEntityPhaseGroup(ctx, ge, stmts)
		}
		return e.executeGroupedChunksWithDrain(ctx, ge, stmts)
	}
	for _, stmt := range stmts {
		if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
			return err
		}
	}
	return nil
}

func (e nornicDBPhaseGroupExecutor) executeSequentialRetractPhase(
	ctx context.Context,
	stmts []sourcecypher.Statement,
) error {
	for i, stmt := range stmts {
		// A Drain-marked statement with an EMPTY DrainVar is a bounded
		// mixed-phase relationship retract (the Helm/GitLab/Atlantis
		// structural-edge shape): Drain means "run me as my own autocommit
		// statement", never "bounded drain loop" — there is no trailing
		// DETACH DELETE <var> to rewrite, and BuildBoundedRetractDrainCypher
		// rejects the empty var. Before this split, an all-retract
		// structural_edges phase (every sibling MERGE upsert absent because a
		// later generation removed all relationships) failed the whole
		// canonical write with "drainVar must not be empty" instead of
		// retracting (raised in review on #5155). Only DrainVar-carrying
		// statements (unbounded full-refresh DETACH DELETE) take the LIMIT
		// drain loop below.
		if stmt.Drain && stmt.DrainVar == "" {
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.executeAutocommitRetract(ctx, stmt, i+1, len(stmts), statementSummary); err != nil {
				return err
			}
			continue
		}
		if stmt.Drain && e.drainReader != nil {
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.executeDrainLoop(ctx, stmt, i+1, len(stmts), statementSummary); err != nil {
				return err
			}
			continue
		}
		chunks := sourcecypher.ChunkPositiveStringSliceRetractStatement(
			stmt,
			sourcecypher.DefaultPositiveRetractStringSliceBatchSize,
		)
		for chunkIndex, chunk := range chunks {
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{chunk})
			if err := e.inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
				return fmt.Errorf(
					"phase-group retract statement %d/%d part %d/%d (duration=%s, first_statement=%q): %w",
					i+1,
					len(stmts),
					chunkIndex+1,
					len(chunks),
					time.Since(statementStart),
					statementSummary,
					err,
				)
			}
		}
	}
	return nil
}

// executeDrainLoop converts a Drain-marked full-refresh retract statement into
// a bounded drain loop for NornicDB. It rewrites the trailing DETACH DELETE
// clause to include a LIMIT and RETURN count(__drained), then repeats until
// __drained == 0 or the safety cap is exceeded.
//
// Concurrency safety: the retract conflict_domain is scope (one worker per
// scope). Deletes are idempotent — repeated deletes of already-absent nodes
// return 0 without error. The loop is therefore concurrency-safe: no other
// worker touches the same scope's prior-generation nodes during a retract.
func (e nornicDBPhaseGroupExecutor) executeDrainLoop(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
	statementSummary string,
) error {
	batch := e.retractBatchSize
	if batch <= 0 {
		batch = defaultNornicDBCanonicalRetractBatchSize
	}

	drainCypher, err := sourcecypher.BuildBoundedRetractDrainCypher(stmt.Cypher, stmt.DrainVar, "__retract_batch")
	if err != nil {
		return fmt.Errorf(
			"phase-group retract statement %d/%d (first_statement=%q): build drain cypher: %w",
			stmtIdx, stmtTotal, statementSummary, err,
		)
	}

	// Build parameter map with the batch limit appended.
	params := make(map[string]any, len(stmt.Parameters)+1)
	for k, v := range stmt.Parameters {
		params[k] = v
	}
	params["__retract_batch"] = int64(batch)

	// Safety cap: upper bound on total nodes that could exist in the worst case.
	// We use a generous 5_000_000 node ceiling plus a margin of 2 iterations to
	// account for nodes created by concurrent writes during the drain.
	const drainNodeCeiling = 5_000_000
	maxIterations := (drainNodeCeiling/batch + 2)

	var totalDrained, totalNodesDeleted, totalRelsDeleted int64
	phaseStart := time.Now()
	for iteration := 1; ; iteration++ {
		if iteration > maxIterations {
			return fmt.Errorf(
				"phase-group retract statement %d/%d drain loop safety cap exceeded after %d iterations (%d nodes drained, batch=%d, first_statement=%q): drain did not converge",
				stmtIdx, stmtTotal, iteration-1, totalDrained, batch, statementSummary,
			)
		}

		iterStart := time.Now()
		result, runErr := e.drainReader.RunWrite(ctx, drainCypher, params)
		iterDuration := time.Since(iterStart)
		if runErr != nil {
			return fmt.Errorf(
				"phase-group retract statement %d/%d drain iteration %d (total_drained=%d, duration=%s, first_statement=%q): %w",
				stmtIdx, stmtTotal, iteration, totalDrained, iterDuration, statementSummary, runErr,
			)
		}

		drained := drainedCount(result.Rows)
		totalDrained += drained
		totalNodesDeleted += result.NodesDeleted
		totalRelsDeleted += result.RelationshipsDeleted

		slog.Debug(
			"nornicdb retract drain iteration",
			"statement_index", stmtIdx,
			"statement_count", stmtTotal,
			"iteration", iteration,
			"drained", drained,
			"total_drained", totalDrained,
			"batch", batch,
			"duration_s", iterDuration.Seconds(),
		)

		if drained == 0 {
			break
		}
	}

	// Record reconciliation-drift retraction counters so
	// eshu_dp_reconciliation_drift_retractions_total accumulates the same
	// total it would have under the old single-statement Execute path. The
	// call is a no-op when instruments is nil or the statement is not marked
	// as a drift-retract statement by annotateReconciliationDriftWritePhases.
	sourcecypher.RecordReconciliationDriftRetractions(
		ctx,
		e.instruments,
		stmt,
		totalNodesDeleted,
		totalRelsDeleted,
	)

	slog.Info(
		"nornicdb retract drain completed",
		"statement_index", stmtIdx,
		"statement_count", stmtTotal,
		"total_drained", totalDrained,
		"batch", batch,
		"duration_s", time.Since(phaseStart).Seconds(),
		"first_statement", statementSummary,
	)
	return nil
}

// drainedCount reads the __drained int64 counter from the first result row.
// It returns 0 if the row is absent or the key is missing.
func drainedCount(rows []map[string]any) int64 {
	if len(rows) == 0 {
		return 0
	}
	v, ok := rows[0]["__drained"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func (e nornicDBPhaseGroupExecutor) executeEntityPhaseGroup(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	labelStats := make(map[string]*entityPhaseLabelStats)
	phase := statementPhase(stmts)
	grouped := make([]sourcecypher.Statement, 0, len(stmts))
	groupedLabel := ""
	flushGrouped := func() error {
		if len(grouped) == 0 {
			return nil
		}
		err := e.executeGroupedChunksConcurrentlyObserved(
			ctx,
			ge,
			grouped,
			e.phaseGroupStatementLimit(grouped),
			e.entityPhaseConcurrency,
			func(chunk []sourcecypher.Statement, chunkDuration time.Duration) {
				label := entityStatementLabel(chunk[0])
				stats := ensureEntityPhaseLabelStats(labelStats, phase, label, chunk[0])
				stats.recordChunk(chunk, chunkDuration)
				logEntityPhaseLabelSummaryIfDue(stats, false)
			},
		)
		grouped = grouped[:0]
		groupedLabel = ""
		return err
	}

	for i, stmt := range stmts {
		if stmt.Operation == sourcecypher.OperationCanonicalRetract {
			if err := flushGrouped(); err != nil {
				return err
			}
			// Empty-DrainVar Drain statements are bounded relationship
			// retracts that must run as one autocommit statement, never the
			// bounded drain loop (which requires a DrainVar to rewrite the
			// trailing DETACH DELETE). Same split as
			// executeSequentialRetractPhase; raised in review on #5155.
			if stmt.Drain && stmt.DrainVar == "" {
				statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
				if err := e.executeAutocommitRetract(ctx, stmt, i+1, len(stmts), statementSummary); err != nil {
					return err
				}
				continue
			}
			if stmt.Drain && e.drainReader != nil {
				statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
				if err := e.executeDrainLoop(ctx, stmt, i+1, len(stmts), statementSummary); err != nil {
					return err
				}
				continue
			}
			chunks := sourcecypher.ChunkPositiveStringSliceRetractStatement(
				stmt,
				sourcecypher.DefaultPositiveRetractStringSliceBatchSize,
			)
			for chunkIndex, chunk := range chunks {
				statementStart := time.Now()
				statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{chunk})
				if err := e.inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
					return fmt.Errorf(
						"phase-group retract statement %d/%d part %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
						i+1,
						len(stmts),
						chunkIndex+1,
						len(chunks),
						phase,
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
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
				return fmt.Errorf(
					"phase-group singleton statement %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
					i+1,
					len(stmts),
					phase,
					time.Since(statementStart),
					statementSummary,
					err,
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
			stats := ensureEntityPhaseLabelStats(labelStats, phase, entityStatementLabel(stmt), stmt)
			stats.recordSingleton(stmt, time.Since(statementStart))
			logEntityPhaseLabelSummaryIfDue(stats, false)
			continue
		}
		stmtLabel := entityStatementLabel(stmt)
		if len(grouped) > 0 && stmtLabel != groupedLabel {
			completedLabel := groupedLabel
			if err := flushGrouped(); err != nil {
				return err
			}
			logEntityPhaseLabelSummaryIfDue(labelStats[completedLabel], true)
		}
		grouped = append(grouped, stmt)
		if groupedLabel == "" {
			groupedLabel = stmtLabel
		}
		if len(grouped) >= e.entityFlushTrigger(grouped) {
			if err := flushGrouped(); err != nil {
				return err
			}
		}
	}

	if err := flushGrouped(); err != nil {
		return err
	}
	logEntityPhaseLabelSummaries(labelStats, true)
	return nil
}

func (e nornicDBPhaseGroupExecutor) executeGroupedChunks(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	maxStatements int,
) error {
	return e.executeGroupedChunksObserved(ctx, ge, stmts, maxStatements, nil)
}

func (e nornicDBPhaseGroupExecutor) executeGroupedChunksObserved(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	maxStatements int,
	observer func([]sourcecypher.Statement, time.Duration),
) error {
	totalChunks := (len(stmts) + maxStatements - 1) / maxStatements
	for start := 0; start < len(stmts); start += maxStatements {
		end := start + maxStatements
		if end > len(stmts) {
			end = len(stmts)
		}
		chunkIndex := (start / maxStatements) + 1
		chunkStart := time.Now()
		chunk := stmts[start:end]
		statementSummary := summarizePhaseGroupChunk(chunk)
		err := ge.ExecuteGroup(ctx, sanitizedPhaseGroupChunk(chunk))
		chunkDuration := time.Since(chunkStart)
		if err != nil {
			return fmt.Errorf(
				"phase-group chunk %d/%d (statements %d-%d of %d, size=%d, duration=%s, first_statement=%q): %w",
				chunkIndex,
				totalChunks,
				start+1,
				end,
				len(stmts),
				end-start,
				chunkDuration,
				statementSummary,
				err,
			)
		}
		if observer != nil {
			observer(chunk, chunkDuration)
		}
		slog.Info(
			"nornicdb phase-group chunk completed",
			"chunk_index", chunkIndex,
			"chunk_count", totalChunks,
			"statement_start", start+1,
			"statement_end", end,
			"statement_count", end-start,
			"duration_s", chunkDuration.Seconds(),
			"first_statement", statementSummary,
		)
	}
	return nil
}
