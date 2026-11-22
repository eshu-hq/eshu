// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import (
	"errors"
	"fmt"
	"time"
)

// SourcePacedRelease is the cause the projector hands to Fail when a claimed
// item's source is latched slowed and the item is released before any
// projection write (#10882 F8). It implements SourcePacedError, so the queue
// retries it without spending either attempt budget, never dead-letters it,
// and honors RetryAfter as the earliest the item may be claimed again.
type SourcePacedRelease struct {
	// Source is the slowed source system.
	Source string
	// RetryAfter is the grant delay the verdict carried.
	RetryAfter time.Duration
}

func (e SourcePacedRelease) Error() string {
	return fmt.Sprintf("reducer source paced: source %q slowed, releasing claim before projection writes (retry after %s)",
		e.Source, e.RetryAfter)
}

// Retryable reports true so Fail releases the claim instead of dead-lettering.
func (SourcePacedRelease) Retryable() bool { return true }

// IsSourcePaced opts into the SourcePacedError contract.
func (SourcePacedRelease) IsSourcePaced() bool { return true }

// RetryAfterHint reports the earliest delay before the item may be claimed again.
func (e SourcePacedRelease) RetryAfterHint() time.Duration { return e.RetryAfter }

// RetryAfterHint returns the retry hint err carries through any wrapping, or
// zero when it carries none.
func RetryAfterHint(err error) time.Duration {
	var hinted interface{ RetryAfterHint() time.Duration }
	if errors.As(err, &hinted) {
		return hinted.RetryAfterHint()
	}
	return 0
}
