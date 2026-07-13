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

// executeDrainLoop converts a Drain-marked full-refresh retract statement into
// a bounded drain loop for NornicDB. It rewrites the trailing DETACH DELETE
// clause to include a LIMIT and RETURN count(__drained), then repeats until
// __drained == 0 or the safety cap is exceeded.
//
// Concurrency safety: the retract conflict_domain is scope (one worker per
// scope). Deletes are idempotent — repeated deletes of already-absent nodes
// return 0 without error. The loop is therefore concurrency-safe: no other
// worker touches the same scope's prior-generation nodes during a retract.
func (e PhaseGroupExecutor) executeDrainLoop(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
	statementSummary string,
) error {
	batch := e.RetractBatchSize
	if batch <= 0 {
		batch = DefaultCanonicalRetractBatchSize
	}

	drainCypher, err := sourcecypher.BuildBoundedRetractDrainCypher(stmt.Cypher, stmt.DrainVar, "__retract_batch")
	if err != nil {
		return fmt.Errorf(
			"phase-group retract statement %d/%d (first_statement=%q): build drain cypher: %w",
			stmtIdx, stmtTotal, statementSummary, err,
		)
	}

	params := make(map[string]any, len(stmt.Parameters)+1)
	for key, value := range stmt.Parameters {
		params[key] = value
	}
	params["__retract_batch"] = int64(batch)

	const drainNodeCeiling = 5_000_000
	maxIterations := drainNodeCeiling/batch + 2

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
		result, runErr := e.DrainReader.RunWrite(ctx, drainCypher, params)
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

	sourcecypher.RecordReconciliationDriftRetractions(
		ctx,
		e.Instruments,
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

func drainedCount(rows []map[string]any) int64 {
	if len(rows) == 0 {
		return 0
	}
	value, ok := rows[0]["__drained"]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case int64:
		return number
	case int:
		return int64(number)
	case float64:
		return int64(number)
	}
	return 0
}
