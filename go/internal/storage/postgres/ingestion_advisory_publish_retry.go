// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const publicAdvisoryPublishAttempts = 3

type publicAdvisoryTerminalError struct {
	reason string
	cause  error
}

func (e publicAdvisoryTerminalError) Error() string {
	switch e.reason {
	case "receipt_incomplete":
		return "public advisory generation has no complete staged receipt"
	default:
		return "public advisory publication failed after three same-claim attempts"
	}
}

func (e publicAdvisoryTerminalError) Unwrap() error         { return e.cause }
func (e publicAdvisoryTerminalError) TerminalFailure() bool { return true }
func (e publicAdvisoryTerminalError) FailureClass() string {
	if e.reason == "receipt_incomplete" {
		return "public_advisory_receipt_incomplete"
	}
	return "public_advisory_publish_exhausted"
}

func (e publicAdvisoryTerminalError) FailureOperation() string {
	return "public_advisory_publish"
}

func publishPublicAdvisoryWithRetry(ctx context.Context,
	authority PublicAdvisoryGenerationAuthority, mutation workflow.ClaimMutation,
	scopeID, generationID string,
) (bool, error) {
	for attempt := 1; attempt <= publicAdvisoryPublishAttempts; attempt++ {
		published, err := authority.PublishCompletePublicAdvisoryGeneration(ctx, mutation,
			scopeID, generationID)
		if err == nil {
			return published, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if attempt == publicAdvisoryPublishAttempts {
			return false, publicAdvisoryTerminalError{reason: "retry_exhausted", cause: err}
		}
		delay := time.Duration(attempt) * 100 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	return false, publicAdvisoryTerminalError{reason: "retry_exhausted"}
}
