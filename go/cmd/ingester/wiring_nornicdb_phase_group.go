package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
	// runtime default is `runtime.NumCPU()` clamped to
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
		return e.executeGroupedChunks(ctx, ge, stmts, e.phaseGroupStatementLimit(stmts))
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
		statementStart := time.Now()
		statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
		if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
			return fmt.Errorf(
				"phase-group retract statement %d/%d (duration=%s, first_statement=%q): %w",
				i+1,
				len(stmts),
				time.Since(statementStart),
				statementSummary,
				err,
			)
		}
	}
	return nil
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
			statementStart := time.Now()
			statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
			if err := e.inner.Execute(ctx, sanitizedStatement(stmt)); err != nil {
				return fmt.Errorf(
					"phase-group retract statement %d/%d (phase=%s, duration=%s, first_statement=%q): %w",
					i+1,
					len(stmts),
					phase,
					time.Since(statementStart),
					statementSummary,
					err,
				)
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
