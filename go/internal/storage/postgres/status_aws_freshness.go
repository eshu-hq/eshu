package postgres

import (
	"context"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const awsFreshnessStatusCountsQuery = `
SELECT status, COUNT(*) AS count
FROM aws_freshness_triggers
GROUP BY status
ORDER BY status
`

const awsFreshnessOldestQueuedAgeQuery = `
SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM ($1::timestamptz - MIN(received_at))), 0), 0) AS oldest_queued_age_seconds
FROM aws_freshness_triggers
WHERE status = 'queued'
`

func readAWSFreshnessSnapshot(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) (statuspkg.AWSFreshnessSnapshot, error) {
	counts, err := listNamedCounts(ctx, queryer, awsFreshnessStatusCountsQuery, "list AWS freshness status counts")
	if err != nil {
		return statuspkg.AWSFreshnessSnapshot{}, err
	}
	oldestQueuedAge, err := readAWSFreshnessOldestQueuedAge(ctx, queryer, asOf.UTC())
	if err != nil {
		return statuspkg.AWSFreshnessSnapshot{}, err
	}
	return statuspkg.AWSFreshnessSnapshot{
		StatusCounts:    counts,
		OldestQueuedAge: oldestQueuedAge,
	}, nil
}

func readAWSFreshnessOldestQueuedAge(ctx context.Context, queryer Queryer, asOf time.Time) (time.Duration, error) {
	rows, err := queryer.QueryContext(ctx, awsFreshnessOldestQueuedAgeQuery, asOf.UTC())
	if err != nil {
		return 0, fmt.Errorf("read AWS freshness oldest queued age: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("read AWS freshness oldest queued age: %w", err)
		}
		return 0, nil
	}
	var seconds float64
	if err := rows.Scan(&seconds); err != nil {
		return 0, fmt.Errorf("read AWS freshness oldest queued age: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read AWS freshness oldest queued age: %w", err)
	}
	if seconds < 0 {
		seconds = 0
	}
	return durationFromSeconds(seconds), nil
}
