// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

// GenerationDeadLetterSnapshot captures collector generation commit
// failures that were quarantined before projector work items existed.
type GenerationDeadLetterSnapshot struct {
	DeadLetter          int
	ReplayRequested     int
	ReplayAttempts      int
	OldestDeadLetterAge time.Duration
}

func CloneGenerationDeadLetterSnapshot(
	snapshot GenerationDeadLetterSnapshot,
) GenerationDeadLetterSnapshot {
	return GenerationDeadLetterSnapshot{
		DeadLetter:          nonNegativeCount(snapshot.DeadLetter),
		ReplayRequested:     nonNegativeCount(snapshot.ReplayRequested),
		ReplayAttempts:      nonNegativeCount(snapshot.ReplayAttempts),
		OldestDeadLetterAge: shared.NonNegativeDuration(snapshot.OldestDeadLetterAge),
	}
}

func RenderGenerationDeadLetterLine(snapshot GenerationDeadLetterSnapshot) string {
	snapshot = CloneGenerationDeadLetterSnapshot(snapshot)
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
