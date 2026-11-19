// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"context"
	"errors"
	"time"
)

// generationRetentionPhaseSeconds converts phase durations to seconds for the
// cycle log.
func generationRetentionPhaseSeconds(phases map[string]time.Duration) map[string]float64 {
	seconds := make(map[string]float64, len(phases))
	for phase, duration := range phases {
		seconds[phase] = duration.Seconds()
	}
	return seconds
}

func generationRetentionRowsPrunedTotal(rows map[string]int64) int64 {
	var total int64
	for _, count := range rows {
		total += count
	}
	return total
}

// generationRetentionSkippedRetryInterval returns the retry delay after
// consecutiveSkipped consecutive skipped-only passes: the base interval
// doubling per pass, capped at the poll interval (#7398). The cap keeps a
// permanently-blocked backlog on the configured cadence, and it keeps a
// shorter-than-base poll interval unchanged.
func generationRetentionSkippedRetryInterval(poll time.Duration, consecutiveSkipped int) time.Duration {
	wait := defaultGenerationRetentionSkippedRetryBaseInterval
	if wait <= 0 {
		return poll
	}
	for i := 1; i < consecutiveSkipped; i++ {
		wait *= 2
		if wait <= 0 || wait >= poll {
			return poll
		}
	}
	if wait > poll {
		return poll
	}
	return wait
}

func generationRetentionSkippedTotal(skipped map[string]int) int {
	var total int
	for _, count := range skipped {
		total += count
	}
	return total
}

func contextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
