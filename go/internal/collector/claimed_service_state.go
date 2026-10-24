// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// commitCollectedWithState commits a generation that carries collector-owned
// state. The state must commit with the generation, so a committer that cannot
// persist it fails the claim instead of committing the facts alone: a
// generation committed without its state would read as fully collected.
func (s ClaimedService) commitCollectedWithState(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	collected CollectedGeneration,
) error {
	committer, ok := s.Committer.(StateClaimedCommitter)
	if !ok {
		if err := cleanupCollectedFactStream(collected); err != nil {
			return err
		}
		return errors.New("claim-aware collector committer must support generation state")
	}
	if err := committer.CommitClaimedScopeGenerationWithState(
		ctx,
		mutation,
		collected.Scope,
		collected.Generation,
		collected.Facts,
		collected.FactStreamErr,
		collected.StateWriter,
	); err != nil {
		return fmt.Errorf("commit claimed generation with state: %w", err)
	}
	return nil
}
