// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var errInnerExecutorRequired = errors.New("nornicdb phase-group inner executor is required")

// PhaseGroupExecutor exposes NornicDB's bounded dependency-phase write path.
// It intentionally implements cypher.PhaseGroupExecutor without implementing
// cypher.GroupExecutor, because NornicDB does not provide read-your-writes for
// the canonical writer's whole-materialization transaction shape.
type PhaseGroupExecutor struct {
	Inner                    sourcecypher.Executor
	MaxStatements            int
	DirectoryMaxStatements   int
	FileMaxStatements        int
	EntityMaxStatements      int
	EntityLabelMaxStatements map[string]int
	// EntityPhaseConcurrency caps how many canonical entity-phase grouped
	// chunks may run in parallel against the inner GroupExecutor. The
	// runtime default is `cpubudget.UsableCPUs()` (cgroup-aware CPU count) clamped to
	// `EntityPhaseConcurrencyCap`, so most callers route through
	// the streaming dispatcher in phase_group_executor_streaming.go.
	// A value of zero or one is an explicit serial override: it pins
	// ExecutePhaseGroup to the legacy per-flush executeEntityPhaseGroup
	// path so callers that need deterministic chunk ordering (or that are
	// debugging a streaming-specific regression) can opt out. Cross-chunk
	// safety inside an entity label rests on disjoint entity_id MERGE keys
	// plus the file_path MATCH-only contract; see
	// executeGroupedChunksConcurrentlyObserved for the full safety
	// argument.
	EntityPhaseConcurrency int

	// DrainReader drives the bounded drain loop for full-refresh DETACH DELETE
	// statements marked with Drain=true. When nil, Drain statements fall back
	// to a single Execute call (backward-compatible with existing tests that
	// do not wire a reader). In production, rawExecutor (ingesterNeo4jExecutor)
	// is threaded here via retractDrainReader.
	DrainReader DrainReader

	// RetractBatchSize is the LIMIT applied to each drain-loop iteration.
	// Controlled by ESHU_CANONICAL_RETRACT_BATCH.
	RetractBatchSize int

	// Instruments carries the OTEL metric handles for recording reconciliation
	// drift retraction counters after each drain loop completes. May be nil in
	// tests that do not wire telemetry; RecordReconciliationDriftRetractions
	// is a no-op when Instruments is nil.
	Instruments *telemetry.Instruments
}

// Execute sanitizes and runs one canonical statement through the inner executor.
func (e PhaseGroupExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.Inner == nil {
		return errInnerExecutorRequired
	}
	if err := e.Inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
		return fmt.Errorf("execute canonical statement: %w", err)
	}
	return nil
}

// ExecutePhaseGroup commits one dependency phase with bounded NornicDB transactions.
func (e PhaseGroupExecutor) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if e.Inner == nil {
		return errInnerExecutorRequired
	}
	if ge, ok := e.Inner.(sourcecypher.GroupExecutor); ok {
		if allStatementsUseOperation(stmts, sourcecypher.OperationCanonicalRetract) {
			return e.executeSequentialRetractPhase(ctx, stmts)
		}
		if statementPhaseUsesEntityLabelStats(statementPhase(stmts)) {
			if e.EntityPhaseConcurrency > 1 {
				return e.ExecuteEntityPhaseGroupStreaming(ctx, ge, stmts)
			}
			return e.executeEntityPhaseGroup(ctx, ge, stmts)
		}
		return e.executeGroupedChunksWithDrain(ctx, ge, stmts)
	}
	for i, stmt := range stmts {
		if err := e.Inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
			return fmt.Errorf("execute phase statement %d/%d: %w", i+1, len(stmts), err)
		}
	}
	return nil
}

func (e PhaseGroupExecutor) executeSequentialRetractPhase(
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
		if stmt.Drain && e.DrainReader != nil {
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
			if err := e.Inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
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

func (e PhaseGroupExecutor) executeEntityPhaseGroup(
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
		err := e.ExecuteGroupedChunksConcurrentlyObserved(
			ctx,
			ge,
			grouped,
			e.PhaseGroupStatementLimit(grouped),
			e.EntityPhaseConcurrency,
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
			if stmt.Drain && e.DrainReader != nil {
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
				if err := e.Inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
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
			if err := e.Inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
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
		if len(grouped) >= e.EntityFlushTrigger(grouped) {
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

func (e PhaseGroupExecutor) executeGroupedChunks(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	maxStatements int,
) error {
	return e.executeGroupedChunksObserved(ctx, ge, stmts, maxStatements, nil)
}

func (e PhaseGroupExecutor) executeGroupedChunksObserved(
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
