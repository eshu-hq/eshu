// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// RefinalizeClaimFenceWithRetry retries whole refinalize transactions only when
// the reducer claim fence returns PostgreSQL lock_not_available (55P03). The
// admission budget covers retry work through successful fence acquisition; the
// caller context governs the accepted transaction through commit.
func RefinalizeClaimFenceWithRetry(
	ctx context.Context,
	retryBudget time.Duration,
	logger *slog.Logger,
	attempt func(context.Context, *FenceAdmissionBudget) (recovery.RefinalizeResult, error),
) (recovery.RefinalizeResult, error) {
	if attempt == nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize claim fence retry: attempt is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if retryBudget <= 0 {
		retryBudget = DefaultRefinalizeClaimFenceRetryBudget
	}
	admission := NewFenceAdmissionBudget(ctx, retryBudget)
	defer admission.Close()
	budgetError := func(attempts int, cause string) error {
		elapsed := admission.Elapsed()
		logger.WarnContext(ctx, "refinalize claim fence admission budget exhausted",
			slog.String("outcome", "blocked/claim_fence_busy"), slog.Int("attempts", attempts),
			slog.Duration("retry_elapsed", elapsed), slog.String("phase", "fence_admission"),
			slog.String("cause", cause), slog.String("last_sqlstate", "55P03"))
		return fmt.Errorf("%w after %s and %d attempts", recovery.ErrRefinalizeClaimFenceBusy, elapsed.Round(time.Millisecond), attempts)
	}
	for attempts := 1; ; attempts++ {
		result, err := attempt(admission.Context(), admission)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return recovery.RefinalizeResult{}, err
		}
		if admission.Expired() {
			return recovery.RefinalizeResult{}, budgetError(attempts, "admission_deadline")
		}
		var lockTimeout *ClaimFenceLockTimeoutError
		if !errors.As(err, &lockTimeout) || !coordination.IsLockNotAvailable(err) {
			return recovery.RefinalizeResult{}, err
		}
		elapsed := admission.Elapsed()
		if elapsed >= retryBudget {
			return recovery.RefinalizeResult{}, budgetError(attempts, "last_fence_timeout")
		}
		// #nosec G404 -- jitter spreads retry timing and makes no security decision.
		delay := 100*time.Millisecond + time.Duration(rand.Int64N(401))*time.Millisecond
		if remaining := retryBudget - elapsed; delay > remaining {
			delay = remaining
		}
		logger.InfoContext(ctx, "refinalize claim fence timed out; retrying transaction",
			slog.Int("attempt", attempts), slog.Duration("backoff", delay),
			slog.Duration("retry_elapsed", elapsed), slog.String("sqlstate", "55P03"))
		timer := time.NewTimer(delay)
		select {
		case <-admission.Context().Done():
			timer.Stop()
			if ctx.Err() != nil {
				return recovery.RefinalizeResult{}, fmt.Errorf("refinalize claim fence retry: %w", ctx.Err())
			}
			return recovery.RefinalizeResult{}, budgetError(attempts, "admission_deadline")
		case <-timer.C:
		}
	}
}
