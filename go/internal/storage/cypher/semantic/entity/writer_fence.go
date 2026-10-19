// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// WithGenerationCheck fences every dispatched group on the owning
// generation's freshness (#8628). The check reports whether the write's
// stamped generation is still the scope's active generation; it is the same
// contract the reducer handler's pre-write fence uses, so a run superseded
// after the handler's probe still aborts between groups instead of landing
// stale rows over the active generation's truth. A nil check keeps the
// legacy unfenced run-to-completion path. The writer never retains the
// check's backend: each probe is one read-only primary-key lookup on
// scope_generations, adding no lock, worker, batch, or retry change.
func (w *SemanticEntityWriter) WithGenerationCheck(
	check reducercontract.GenerationFreshnessCheck,
) *SemanticEntityWriter {
	if w == nil {
		return w
	}
	w.generationCheck = check
	return w
}

// fenceSemanticGroup runs one freshness probe before a group dispatches. A
// nil check keeps the legacy path with no probe. A probe error — including
// GenerationNotYetActiveError, which means the generation is about to
// activate rather than superseded — aborts wrapped with %w and fail-closed,
// never as supersession. A stale verdict on a stamped write aborts with
// semantic.ErrGenerationSuperseded; on an unstamped write the verdict is
// ambiguous (the probe ran with empty IDs), so it fails closed with an
// error instead of silently superseding or writing.
func (w *SemanticEntityWriter) fenceSemanticGroup(
	ctx context.Context,
	write semantic.EntityWrite,
) error {
	check := w.generationCheck
	if check == nil {
		return nil
	}
	fresh, err := check(ctx, write.ScopeID, write.GenerationID)
	if err != nil {
		return fmt.Errorf("check semantic generation freshness: %w", sourcecypher.WrapRetryableNeo4jError(err))
	}
	if fresh {
		return nil
	}
	if write.ScopeID == "" || write.GenerationID == "" {
		return errors.New("semantic entity write fenced as superseded but carries no scope/generation stamp")
	}
	return fmt.Errorf(
		"semantic entity write for scope %s generation %s superseded: %w",
		write.ScopeID, write.GenerationID,
		sourcecypher.WrapRetryableNeo4jError(semantic.ErrGenerationSuperseded),
	)
}

// semanticLabeledStatementRows counts the dispatched upsert rows behind the
// partial-write rule: only statements carrying the per-statement entity
// label key count, and only upserts. Retract statements carry repo ID lists
// instead of rows, and the ownership statement carries rows without a label
// key, so neither inflates a partial CanonicalWrites.
func semanticLabeledStatementRows(stmts []sourcecypher.Statement) int {
	total := 0
	for _, stmt := range stmts {
		if stmt.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		if _, ok := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey]; !ok {
			continue
		}
		total += semanticStatementRowCount(stmt)
	}
	return total
}

// dispatchSemanticStatements dispatches the built statements in order,
// fencing before every group on group-capable executors — including the
// first group and retract-only writes — and before every statement on the
// per-statement fallback. It reports committed groups and dispatched labeled
// rows so a fence abort can carry the partial outcome; backend errors keep
// the existing abort with the wrapped backend error.
func (w *SemanticEntityWriter) dispatchSemanticStatements(
	ctx context.Context,
	write semantic.EntityWrite,
	stmts []sourcecypher.Statement,
) (groups int, dispatchedRows int, err error) {
	if len(stmts) == 0 {
		return 0, 0, nil
	}
	if ge, ok := w.executor.(sourcecypher.GroupExecutor); ok {
		// Split oversized writes into sequential row-bounded groups so
		// no single transaction exceeds the backend request ceiling.
		// Groups dispatch in order (retracts lead upserts); a group
		// failure aborts the write and the work item retry replays all
		// groups to convergence.
		split := splitSemanticStatements(stmts, w.groupRowLimit())
		for i, group := range split {
			if fenceErr := w.fenceSemanticGroup(ctx, write); fenceErr != nil {
				return groups, dispatchedRows, fenceErr
			}
			if dispatchErr := ge.ExecuteGroup(ctx, group); dispatchErr != nil {
				return 0, 0, fmt.Errorf("write semantic entities group %d of %d: %w", i+1, len(split), sourcecypher.WrapRetryableNeo4jError(dispatchErr))
			}
			groups++
			dispatchedRows += semanticLabeledStatementRows(group)
		}
		return groups, dispatchedRows, nil
	}
	for _, stmt := range stmts {
		if fenceErr := w.fenceSemanticGroup(ctx, write); fenceErr != nil {
			return 0, dispatchedRows, fenceErr
		}
		if dispatchErr := w.executor.Execute(ctx, stmt); dispatchErr != nil {
			return 0, 0, fmt.Errorf("write semantic entities: %w", sourcecypher.WrapRetryableNeo4jError(dispatchErr))
		}
		dispatchedRows += semanticLabeledStatementRows([]sourcecypher.Statement{stmt})
	}
	return 0, dispatchedRows, nil
}
