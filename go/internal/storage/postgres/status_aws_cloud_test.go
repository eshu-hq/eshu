package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReadAWSCloudScanStatusesMapsNullTimesUTCAndTruncation(t *testing.T) {
	t.Parallel()

	offsetTime := time.Date(2026, 5, 13, 9, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	rows := make([][]any, 0, awsCloudScanStatusLimit+1)
	for i := 0; i < awsCloudScanStatusLimit+1; i++ {
		rows = append(rows, awsCloudScanStatusRow(offsetTime))
	}
	queryer := &fakeQueryer{responses: []fakeRows{{rows: rows}}}

	statuses, truncated, err := readAWSCloudScanStatuses(context.Background(), queryer)
	if err != nil {
		t.Fatalf("readAWSCloudScanStatuses() error = %v, want nil", err)
	}
	if len(statuses) != awsCloudScanStatusLimit {
		t.Fatalf("statuses = %d, want %d", len(statuses), awsCloudScanStatusLimit)
	}
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	row := statuses[0]
	if !row.LastStartedAt.IsZero() || !row.LastCompletedAt.IsZero() || !row.LastSuccessfulAt.IsZero() {
		t.Fatalf("nullable times = %s/%s/%s, want zero values", row.LastStartedAt, row.LastCompletedAt, row.LastSuccessfulAt)
	}
	if row.LastObservedAt.Location() != time.UTC || row.UpdatedAt.Location() != time.UTC {
		t.Fatalf("times not normalized to UTC: observed=%s updated=%s", row.LastObservedAt, row.UpdatedAt)
	}
	if !strings.Contains(queryer.queries[0], "LIMIT $1") {
		t.Fatalf("query = %s, want bounded parameterized limit", queryer.queries[0])
	}
}

func TestReadAWSCloudScanStatusesReturnsQueryError(t *testing.T) {
	t.Parallel()

	want := errors.New("query failed")
	_, _, err := readAWSCloudScanStatuses(
		context.Background(),
		&fakeQueryer{responses: []fakeRows{{err: want}}},
	)
	if !errors.Is(err, want) {
		t.Fatalf("readAWSCloudScanStatuses() error = %v, want %v", err, want)
	}
}

func awsCloudScanStatusRow(observedAt time.Time) []any {
	return []any{
		"aws-prod",
		"123456789012",
		"us-east-1",
		"ecr",
		"partial",
		"committed",
		"budget_exhausted",
		"budget hit",
		int64(51),
		int64(3),
		int64(1),
		int64(10),
		int64(4),
		int64(2),
		true,
		false,
		nil,
		observedAt,
		nil,
		nil,
		observedAt,
	}
}
