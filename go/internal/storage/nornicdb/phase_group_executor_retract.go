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

// executeGroupedChunksWithDrain handles a mixed phase group (e.g. structural
// edges or terraform_state, which carry one or more retracts interleaved with
// upserts). It walks stmts in EMITTED ORDER, accumulating consecutive
// non-retract statements into a pending group. The first time it sees an
// OperationCanonicalRetract statement (Drain-marked or not), it flushes the
// pending group through ge.ExecuteGroup, runs the retract statement autocommit
// in that exact position, then resumes accumulating. Any trailing pending
// group is flushed once the walk completes. This mirrors
// executeEntityPhaseGroup's own flush-then-autocommit-in-position structure
// (see executeMixedPhaseRetractInPosition for the retract-dispatch branch the
// two share) and is the fix for #5680, a P0 with two independent defects in
// the previous hoist-all-Drain-first implementation:
//
//  1. A non-Drain OperationCanonicalRetract statement (e.g. the terraform_state
//     DETACH DELETE sweeps in tfstate_canonical_writer_retract.go, or the
//     migration/attribute-remove retracts) was bundled into the single
//     ge.ExecuteGroup call for "remaining" statements. Every retract DELETE is
//     ExecuteGroup-unsafe on the pinned NornicDB v1.1.11 — it silently
//     under-applies inside a managed transaction, even alone in a
//     one-statement group (docs/public/reference/nornicdb-query-pitfalls.md,
//     "retract DELETEs run through Execute, never ExecuteGroup") — so a stale
//     terraform_state resource never actually got retracted; the DELETE ran,
//     matched rows, but did not persist.
//  2. Every Drain-marked statement was hoisted to run BEFORE any upsert in the
//     phase, regardless of its emitted position. The tfstate MATCHES_STATE
//     edge retract (tfstate_state_match_edge_retract.go) is Drain-marked and
//     its predicate requires `s.generation_id = $generation_id`, but that
//     property is only refreshed by the resource-upsert statement emitted
//     BEFORE it (buildTerraformStateStatements' documented phase order).
//     Hoisting the retract ahead of that upsert made the predicate match zero
//     rows every cycle — a silent no-op, not merely a race.
//
// Order-preserving dispatch fixes both: a retract still never reaches
// ge.ExecuteGroup (defect 1), and it always runs at its actual emitted
// position relative to the upserts around it (defect 2). This is also
// behavior-preserving for structural_edges: every structural-edge family
// (Atlantis, Flux, GitLab, Helm) already emits its Drain retract immediately
// before its own upsert, so order-preserving dispatch produces the identical
// retract-before-own-upsert relationship the old hoist-everything-first
// behavior also happened to provide for that phase — see
// canonical_node_writer_phases.go's buildStructuralEdgeStatements.
func (e PhaseGroupExecutor) executeGroupedChunksWithDrain(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	pending := make([]sourcecypher.Statement, 0, len(stmts))
	flushPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := e.executeGroupedChunks(ctx, ge, pending, e.PhaseGroupStatementLimit(pending)); err != nil {
			return err
		}
		pending = pending[:0]
		return nil
	}

	for i, stmt := range stmts {
		if stmt.Operation != sourcecypher.OperationCanonicalRetract {
			pending = append(pending, stmt)
			continue
		}
		if err := flushPending(); err != nil {
			return err
		}
		if err := e.executeMixedPhaseRetractInPosition(ctx, stmt, i+1, len(stmts)); err != nil {
			return err
		}
	}
	return flushPending()
}

// executeMixedPhaseRetractInPosition runs one OperationCanonicalRetract
// statement autocommit, in its emitted position, never through ge.ExecuteGroup
// (see executeGroupedChunksWithDrain's doc comment for why). It mirrors
// executeEntityPhaseGroup's own retract-dispatch branch exactly, so both
// mixed-phase paths route a given retract shape the same way:
//
//   - Drain-marked with an empty DrainVar (the bounded mixed-phase
//     relationship/node-retract convention, e.g. structural-edge DELETEs and
//     the tfstate MATCHES_STATE edge retract): executeAutocommitRetract, one
//     standalone autocommit statement.
//   - Drain-marked with a DrainVar set and a wired DrainReader (an unbounded
//     full-refresh retract that needs the bounded LIMIT loop): executeDrainLoop.
//   - Everything else — a non-Drain retract (e.g. the terraform_state resource
//     sweeps and the migration relabel), or a Drain statement with DrainVar set
//     but no DrainReader wired — runs through the plain chunked Inner.Execute
//     path, batched the same way ChunkPositiveStringSliceRetractStatement
//     batches executeSequentialRetractPhase's non-drain retracts.
func (e PhaseGroupExecutor) executeMixedPhaseRetractInPosition(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
) error {
	statementSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{stmt})
	if stmt.Drain && stmt.DrainVar == "" {
		return e.executeAutocommitRetract(ctx, stmt, stmtIdx, stmtTotal, statementSummary)
	}
	if stmt.Drain && e.DrainReader != nil {
		return e.executeDrainLoop(ctx, stmt, stmtIdx, stmtTotal, statementSummary)
	}
	chunks := sourcecypher.ChunkPositiveStringSliceRetractStatement(
		stmt,
		sourcecypher.DefaultPositiveRetractStringSliceBatchSize,
	)
	for chunkIndex, chunk := range chunks {
		statementStart := time.Now()
		chunkSummary := summarizePhaseGroupChunk([]sourcecypher.Statement{chunk})
		if err := e.Inner.Execute(ctx, sanitizedStatement(chunk)); err != nil {
			return fmt.Errorf(
				"phase-group mixed-phase retract statement %d/%d part %d/%d (duration=%s, first_statement=%q): %w",
				stmtIdx, stmtTotal, chunkIndex+1, len(chunks), time.Since(statementStart), chunkSummary, err,
			)
		}
	}
	return nil
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
func (e PhaseGroupExecutor) executeAutocommitRetract(
	ctx context.Context,
	stmt sourcecypher.Statement,
	stmtIdx, stmtTotal int,
	statementSummary string,
) error {
	sanitized := sanitizedStatement(stmt)
	if e.DrainReader == nil {
		// No RunWrite-capable executor is wired (some tests / non-Bolt
		// executors). Run the retract as its own statement through the inner
		// executor so it is still never batched with the sibling upsert;
		// correctness does not depend on DrainReader being present.
		if err := e.Inner.Execute(ctx, sanitized); err != nil {
			return fmt.Errorf("execute autocommit retract: %w", err)
		}
		return nil
	}
	start := time.Now()
	result, err := e.DrainReader.RunWrite(ctx, sanitized.Cypher, sanitized.Parameters)
	if err != nil {
		return fmt.Errorf(
			"phase-group autocommit retract statement %d/%d (duration=%s, first_statement=%q): %w",
			stmtIdx, stmtTotal, time.Since(start), statementSummary, err,
		)
	}
	sourcecypher.RecordReconciliationDriftRetractions(
		ctx,
		e.Instruments,
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
