package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadAWSFreshnessSnapshotReturnsStatusCountsAndOldestQueuedAge(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{
			{"claimed", int64(1)},
			{"failed", int64(2)},
			{"queued", int64(3)},
		}},
		{rows: [][]any{{float64(42)}}},
	}}

	snapshot, err := readAWSFreshnessSnapshot(
		context.Background(),
		queryer,
		time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("readAWSFreshnessSnapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.StatusCounts), 3; got != want {
		t.Fatalf("status counts = %d, want %d", got, want)
	}
	if got, want := snapshot.StatusCounts[2].Name, "queued"; got != want {
		t.Fatalf("queued status sort = %q, want %q", got, want)
	}
	if got, want := snapshot.OldestQueuedAge, 42*time.Second; got != want {
		t.Fatalf("OldestQueuedAge = %s, want %s", got, want)
	}
	if !strings.Contains(queryer.queries[0], "FROM aws_freshness_triggers") {
		t.Fatalf("status-count query missing table:\n%s", queryer.queries[0])
	}
	if !strings.Contains(queryer.queries[1], "$1::timestamptz") {
		t.Fatalf("oldest-age query missing typed as-of parameter:\n%s", queryer.queries[1])
	}
}

func TestReadAWSFreshnessOldestQueuedAgeClampsNegativeDuration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{float64(-12)}}},
	}}

	age, err := readAWSFreshnessOldestQueuedAge(
		context.Background(),
		queryer,
		time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("readAWSFreshnessOldestQueuedAge() error = %v, want nil", err)
	}
	if age != 0 {
		t.Fatalf("age = %s, want 0", age)
	}
}
