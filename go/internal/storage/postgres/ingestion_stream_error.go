// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var _ collector.StateClaimedCommitter = IngestionStore{}

// CommitScopeGenerationWithStreamError persists one scope generation and fails
// the transaction if factStreamErr reports a producer error after facts close.
func (s IngestionStore) CommitScopeGenerationWithStreamError(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, factStreamErr, nil, nil, nil)
}

// CommitClaimedScopeGenerationWithStreamError persists one claimed generation
// and fails the same transaction if factStreamErr reports a producer error
// after facts close.
func (s IngestionStore) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	if err := validateClaimMutation(mutation); err != nil {
		drainFacts(factStream)
		return err
	}
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, factStreamErr, nil, nil, nil)
}

// CommitClaimedScopeGenerationWithState persists one claimed generation and
// runs stateWriter inside the same transaction, after the generation row
// exists and before the projector work item is enqueued (#8943). The state
// commits with the generation or not at all: a writer error, a fact stream
// error, or a refused successor rolls both back. The writer does not run when
// the generation is skipped as unchanged or as a finalized replay, because
// nothing new commits then. factStreamErr may be nil.
func (s IngestionStore) CommitClaimedScopeGenerationWithState(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	stateWriter collector.GenerationStateWriter,
) error {
	if stateWriter == nil {
		drainFacts(factStream)
		return errors.New("generation state writer is required")
	}
	if err := validateClaimMutation(mutation); err != nil {
		drainFacts(factStream)
		return err
	}
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, factStreamErr, stateWriter, nil, nil)
}

// writeGenerationState runs the optional state writer on the commit
// transaction. A nil writer is the common case and writes nothing.
func writeGenerationState(ctx context.Context, tx db.Executor, stateWriter collector.GenerationStateWriter) error {
	if stateWriter == nil {
		return nil
	}
	if err := stateWriter(ctx, tx); err != nil {
		return fmt.Errorf("write collector generation state: %w", err)
	}
	return nil
}
