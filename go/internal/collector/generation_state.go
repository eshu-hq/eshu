// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// CommitExecutor is the statement surface a GenerationStateWriter receives. It
// is the open commit transaction of the generation, so a write through it
// commits or rolls back with the generation and never outlives it.
type CommitExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// GenerationStateWriter persists collector-owned state for one generation in
// the generation's own commit transaction. The committer calls it after the
// scope generation row exists, so a table that references scope_generations
// can be written, and before the transaction commits. A returned error rolls
// back the whole generation. The committer does not call it when the
// generation is skipped as unchanged or as a finalized replay, because nothing
// new commits then.
type GenerationStateWriter func(context.Context, CommitExecutor) error

// StateClaimedCommitter persists a claimed generation together with
// collector-owned state in one transaction. A collector that sets
// CollectedGeneration.StateWriter needs a committer that implements it.
type StateClaimedCommitter interface {
	CommitClaimedScopeGenerationWithState(
		context.Context,
		workflow.ClaimMutation,
		scope.IngestionScope,
		scope.ScopeGeneration,
		<-chan facts.Envelope,
		func() error,
		GenerationStateWriter,
	) error
}
