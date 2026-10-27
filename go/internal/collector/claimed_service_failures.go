// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// failRetryable persists a retryable claim failure. operation is the
// claim-stage token recorded in the operation column (for example
// "collect_failure"). A rejected FailClaimRetryable write never drops the
// original cause: a fencing rejection (workflow.ErrClaimRejected) means
// another generation owns the claim, so the stale item is dropped and the
// poll loop continues via errRetryableClaimRecorded; any other write failure
// joins both causes and aborts loudly with no continue sentinel.
func (s ClaimedService) failRetryable(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	item workflow.WorkItem,
	operation string,
	err error,
) error {
	failureClass := classifiedFailureClass(err, operation)
	failed := withFailure(mutation, operation, failureClass, false, err)
	if failed.VisibleAt.IsZero() {
		failed.VisibleAt = s.retryableVisibleAt(err)
	}
	if failErr := s.ControlStore.FailClaimRetryable(ctx, failed); failErr != nil {
		if errors.Is(failErr, workflow.ErrClaimRejected) {
			// Fenced away: another generation owns the claim, so there is
			// nothing left to fail. Drop the stale item and keep polling;
			// recording a retry we do not own would lie about the store.
			// The dispatch cause stays in the error chain for diagnosis, and
			// the warn log above keeps the recurrence visible.
			if s.Logger != nil {
				s.Logger.WarnContext(ctx, "retryable-fail fenced away, dropping stale claim",
					"collector_kind", string(s.CollectorKind),
					"source_system", item.SourceSystem,
					"failure_class", failureClass,
				)
			}
			return errors.Join(errRetryableClaimRecorded, fmt.Errorf("%s: %w", failureClass, err))
		}
		return errors.Join(err, fmt.Errorf("retryable-fail claimed %s work item: %w", s.claimedKindLabel(), failErr))
	}
	s.recordClaimRetry(ctx, item, failureClass)
	s.recordProviderThrottle(ctx, item, failureClass, err)
	return errors.Join(errRetryableClaimRecorded, fmt.Errorf("%s: %w", failureClass, err))
}

// failTerminal persists a terminal claim failure. operation is the
// claim-stage token recorded in the operation column (for example
// "collect_failure"); classFallback seeds the class column only when the
// error chain carries no class of its own. The two stay separate so a class
// constant never leaks into the operation taxonomy.
func (s ClaimedService) failTerminal(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	operation string,
	classFallback string,
	err error,
) error {
	failureClass := classifiedFailureClass(err, classFallback)
	failed := withFailure(mutation, operation, failureClass, true, err)
	if failErr := s.ControlStore.FailClaimTerminal(ctx, failed); failErr != nil {
		return fmt.Errorf("terminal-fail claimed %s work item: %w", s.claimedKindLabel(), failErr)
	}
	return errors.Join(errTerminalClaimRecorded, fmt.Errorf("%s: %w", failureClass, err))
}
