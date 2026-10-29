// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const derivedImageScopePrefix = "image://derived/"

// DerivedImageResultCommitter atomically persists a derived image's source
// facts, protected receipt, exact fact membership, and claim completion.
type DerivedImageResultCommitter interface {
	CommitDerivedImageResult(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) error
}

func (s Service) commitDerivedImageResult(
	ctx context.Context, input ClaimInput, mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope, generation scope.ScopeGeneration,
	result AnalyzerResult, stopHeartbeat context.CancelFunc, heartbeatErr <-chan error,
) error {
	// Source authority must be locked before the work and claim rows. Stop the
	// concurrent heartbeat before entering that source-first transaction.
	stopHeartbeat()
	if err := drainScannerHeartbeatError(heartbeatErr); err != nil {
		return err
	}
	if s.AnalyzerKind != AnalyzerImageUnpacking || s.DerivedCommitter == nil {
		return errors.New("derived image result requires atomic image committer")
	}
	if err := s.DerivedCommitter.CommitDerivedImageResult(ctx, mutation, scopeValue, generation, result.Output.Facts); err != nil {
		s.logCommitFailure(input, err)
		return s.recordFailure(ctx, mutation, input, FailureRetryable, FailureClassCommitFailed, result.Usage)
	}
	s.recordSuccess(ctx, input, result)
	return nil
}
