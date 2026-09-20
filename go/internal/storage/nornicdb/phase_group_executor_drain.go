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
// A bare-label statement is probed once first (#6822): a bounded read asks
// whether any node matches, and when none does the drain is skipped. On
// NornicDB v1.3.3 a DETACH DELETE over a bare-label scan with property
// predicates costs a whole-store scan even when nothing matches, while the
// probe costs at most one scan of its own label, so a retract with nothing to
// delete gets cheaper and one with rows to delete pays one extra read. The
// delete itself is unchanged: the single drain statement rechecks its WHERE
// clause atomically, so a node another attempt refreshed to the current
// generation is never deleted.
//
// The probe dispatches through e.Inner as a sourcecypher.ProbeExecutor
// carrying sourcecypher.OperationCanonicalProbe (#6852), not through
// DrainReader: e.Inner is the same instrumented/backpressure/timeout chain
// every other canonical write goes through, so the probe gets a real
// `neo4j.execute_probe` span and `Neo4jQueryDuration{operation=probe}` point
// instead of the bespoke telemetry the former DrainReader.RunProbe seam had.
// Fail-safe direction is unchanged: no ProbeExecutor on Inner, or any probe
// error, runs the drain unconditionally (mode probe_unsupported /
// probe_failed). Only a definitive found=false skips it (mode probe_skipped).
//
// Concurrency safety: the retract conflict_domain is scope (one projector
// worker per scope while its lease is live), but a stale attempt can overlap a
// replacement after its lease expires. Safety rests on the drain statement
// itself: it re-evaluates its WHERE clause when it deletes, so a node another
// attempt refreshed to the current generation after the probe no longer
// matches and is kept. Deletes are idempotent — repeated deletes of
// already-absent nodes return 0 without error.
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
	probeCypher, probed, err := sourcecypher.BuildBoundedRetractProbeCypher(stmt.Cypher, stmt.DrainVar)
	if err != nil {
		return fmt.Errorf(
			"phase-group retract statement %d/%d (first_statement=%q): build probe cypher: %w",
			stmtIdx, stmtTotal, statementSummary, err,
		)
	}

	params := make(map[string]any, len(stmt.Parameters)+1)
	for key, value := range stmt.Parameters {
		params[key] = value
	}
	params["__retract_batch"] = int64(batch)

	phaseStart := time.Now()
	mode := "single_statement"
	skipDrain := false
	var probeDuration time.Duration
	if probed {
		mode = "probed"
		pe, canProbe := e.Inner.(sourcecypher.ProbeExecutor)
		if !canProbe {
			// No ProbeExecutor on Inner means "unknown", never "zero rows":
			// run the drain unconditionally, same fail-safe direction as a
			// probe error below.
			mode = "probe_unsupported"
			probeDuration = time.Since(phaseStart)
			slog.Warn(
				"nornicdb retract probe unsupported; draining unconditionally",
				"statement_index", stmtIdx,
				"statement_count", stmtTotal,
				"first_statement", statementSummary,
			)
		} else {
			found, err := pe.ExecuteProbe(ctx, sourcecypher.Statement{
				Operation:  sourcecypher.OperationCanonicalProbe,
				Cypher:     probeCypher,
				Parameters: params,
			})
			probeDuration = time.Since(phaseStart)
			switch {
			case err != nil:
				// A failed probe means "unknown", never "zero rows" (the
				// sourcecypher.ProbeExecutor contract): run the drain
				// unconditionally so a probe-only failure cannot fail a
				// projection whose delete would succeed.
				mode = "probe_failed"
				slog.Warn(
					"nornicdb retract probe failed; draining unconditionally",
					"statement_index", stmtIdx,
					"statement_count", stmtTotal,
					"probe_duration_s", probeDuration.Seconds(),
					"first_statement", statementSummary,
					"error", err,
				)
			case !found:
				mode = "probe_skipped"
				skipDrain = true
			}
		}
	}

	const drainNodeCeiling = 5_000_000
	maxIterations := drainNodeCeiling/batch + 2

	var totalDrained, totalNodesDeleted, totalRelsDeleted int64
	for iteration := 1; !skipDrain; iteration++ {
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
		"probe_duration_s", probeDuration.Seconds(),
		"first_statement", statementSummary,
		"mode", mode,
	)
	return nil
}

// drainedCount returns the __drained value of a drain result, or 0 when it
// is missing or not numeric.
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
