// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"time"
)

// CollectorGenerationDeadLetterSnapshot captures collector generation commit
// failures that were quarantined before projector work items existed.
type CollectorGenerationDeadLetterSnapshot struct {
	DeadLetter          int
	ReplayRequested     int
	ReplayAttempts      int
	OldestDeadLetterAge time.Duration
}

func cloneCollectorGenerationDeadLetterSnapshot(
	snapshot CollectorGenerationDeadLetterSnapshot,
) CollectorGenerationDeadLetterSnapshot {
	return CollectorGenerationDeadLetterSnapshot{
		DeadLetter:          nonNegativeCount(snapshot.DeadLetter),
		ReplayRequested:     nonNegativeCount(snapshot.ReplayRequested),
		ReplayAttempts:      nonNegativeCount(snapshot.ReplayAttempts),
		OldestDeadLetterAge: nonNegativeDuration(snapshot.OldestDeadLetterAge),
	}
}

func renderCollectorGenerationDeadLetterLine(snapshot CollectorGenerationDeadLetterSnapshot) string {
	snapshot = cloneCollectorGenerationDeadLetterSnapshot(snapshot)
	return fmt.Sprintf(
		"Collector generation dead letters: dead_letter=%d replay_requested=%d replay_attempts=%d oldest=%s",
		snapshot.DeadLetter,
		snapshot.ReplayRequested,
		snapshot.ReplayAttempts,
		snapshot.OldestDeadLetterAge,
	)
}

func nonNegativeCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
