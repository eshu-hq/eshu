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
// When no drain reader is wired (some tests), Drain statements fall through to
// the grouped path unchanged, matching prior behavior.
func (e nornicDBPhaseGroupExecutor) executeGroupedChunksWithDrain(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	if e.drainReader == nil {
		return e.executeGroupedChunks(ctx, ge, stmts, e.phaseGroupStatementLimit(stmts))
	}

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
func (e nornicDBPhaseGroupExecutor) executeAutocommitRetract(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
	statementSummary string,
) error {
	start := time.Now()
	result, err := e.drainReader.RunWrite(ctx, stmt.Cypher, stmt.Parameters)
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
