// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scalars"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

const collectorGenerationDeadLetterStatusQuery = `
SELECT
    COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
    COUNT(*) FILTER (WHERE status = 'replay_requested') AS replay_requested_count,
    COALESCE(SUM(replay_count) FILTER (WHERE status IN ('dead_letter', 'replay_requested')), 0) AS replay_attempt_count,
    COALESCE(
        GREATEST(
            EXTRACT(EPOCH FROM (
                $1 - MIN(last_dead_lettered_at) FILTER (WHERE status IN ('dead_letter', 'replay_requested'))
            )),
            0
        ),
        0
    ) AS oldest_dead_letter_age_seconds
FROM collector_generation_dead_letters
`

func readCollectorGenerationDeadLetterSnapshot(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) (collector.GenerationDeadLetterSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, collectorGenerationDeadLetterStatusQuery, asOf.UTC())
	if err != nil {
		return collector.GenerationDeadLetterSnapshot{}, fmt.Errorf("read collector generation dead-letter snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return collector.GenerationDeadLetterSnapshot{}, fmt.Errorf("read collector generation dead-letter snapshot: %w", err)
		}
		return collector.GenerationDeadLetterSnapshot{}, nil
	}

	var deadLetterCount int64
	var replayRequestedCount int64
	var replayAttemptCount int64
	var oldestAgeSeconds float64
	if err := rows.Scan(&deadLetterCount, &replayRequestedCount, &replayAttemptCount, &oldestAgeSeconds); err != nil {
		return collector.GenerationDeadLetterSnapshot{}, fmt.Errorf("read collector generation dead-letter snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return collector.GenerationDeadLetterSnapshot{}, fmt.Errorf("read collector generation dead-letter snapshot: %w", err)
	}

	return collector.GenerationDeadLetterSnapshot{
		DeadLetter:          int(deadLetterCount),
		ReplayRequested:     int(replayRequestedCount),
		ReplayAttempts:      int(replayAttemptCount),
		OldestDeadLetterAge: scalars.DurationFromSeconds(oldestAgeSeconds),
	}, nil
}
