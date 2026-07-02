// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// executeGroupedChunksWithDrain handles a mixed phase group (e.g. structural
// edges, which carry a relationship retract plus an upsert). Any Drain-marked
// statement runs first as a standalone autocommit statement (outside the grouped
// ExecuteWrite transaction), then the remaining statements run through the normal
// grouped-chunk path. Autocommit is required for two reasons: an UNWIND
// relationship DELETE inside the grouped explicit transaction silently no-ops on
// commit (#4476), and running it separately preserves retract-before-upsert
// ordering (Drain statements are emitted before their sibling upsert, and the
// generation fence makes the retract idempotent, so committing it ahead of the
// upsert is safe). Mixed-phase retracts target a dedicated, small edge type, so
// a single autocommit DELETE is fast — no LIMIT drain loop is needed here (that
// is reserved for all-retract node phases via executeSequentialRetractPhase).
//
// A Drain-marked statement is NEVER added to the grouped chunk, even when no
// drain reader is wired: grouping the relationship DELETE with its sibling
// upsert inside one ExecuteWrite transaction is exactly the no-op this PR fixes.
// executeAutocommitRetract runs it outside the group in every case.
func (e nornicDBPhaseGroupExecutor) executeGroupedChunksWithDrain(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	remaining := make([]sourcecypher.Statement, 0, len(stmts))
	for i, stmt := range stmts {
		if !stmt.Drain {
			remaining = append(remaining, stmt)
			continue
		}
		statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
		if err := e.executeAutocommitRetract(ctx, stmt, i+1, len(stmts), statementSummary); err != nil {
			return err
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	return e.executeGroupedChunks(ctx, ge, remaining, e.phaseGroupStatementLimit(remaining))
}

// executeAutocommitRetract runs a Drain-marked mixed-phase retract as a single
// standalone autocommit statement. Unlike executeDrainLoop (used for all-retract
// node phases, which bounds a potentially huge full-refresh delete with a LIMIT
// loop), a mixed-phase relationship retract targets a dedicated, small edge type
// (e.g. HELM_VALUE_REFERENCE), so one autocommit DELETE is fast and correct.
// Autocommit is required: the same DELETE inside the grouped ExecuteWrite
// transaction silently no-ops on commit (#4476).
//
// The statement is sanitized before it reaches the driver: canonical-write
// statements carry `_eshu_*` phase metadata in their parameters, and the
// sanitize contract warns those unreferenced keys can make NornicDB deletes
// no-op. The original (unsanitized) statement is retained for the drift-retract
// telemetry, which reads that metadata to classify the statement.
func (e nornicDBPhaseGroupExecutor) executeAutocommitRetract(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
	statementSummary string,
) error {
	sanitized := sanitizedStatement(stmt)
	if e.drainReader == nil {
		// No RunWrite-capable executor is wired (some tests / non-Bolt
		// executors). Run the retract as its own statement through the inner
		// executor so it is still never batched with the sibling upsert;
		// correctness does not depend on drainReader being present.
		return e.inner.Execute(ctx, sanitized)
	}
	start := time.Now()
	result, err := e.drainReader.RunWrite(ctx, sanitized.Cypher, sanitized.Parameters)
	if err != nil {
		return fmt.Errorf(
			"phase-group autocommit retract statement %d/%d (duration=%s, first_statement=%q): %w",
			stmtIdx, stmtTotal, time.Since(start), statementSummary, err,
		)
	}
	sourcecypher.RecordReconciliationDriftRetractions(
		ctx,
		e.instruments,
		stmt,
		result.NodesDeleted,
		result.RelationshipsDeleted,
	)
	slog.Info(
		"nornicdb autocommit retract completed",
		"statement_index", stmtIdx,
		"statement_count", stmtTotal,
		"rels_deleted", result.RelationshipsDeleted,
		"duration_s", time.Since(start).Seconds(),
		"first_statement", statementSummary,
	)
	return nil
}
