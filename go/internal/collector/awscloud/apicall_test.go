package awscloud

import (
	"context"
	"testing"
)

func TestAPICallRecorderContextCapturesBoundedStats(t *testing.T) {
	t.Parallel()

	boundary := Boundary{
		CollectorInstanceID: "aws-prod",
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         ServiceECR,
		GenerationID:        "generation-1",
		FencingToken:        7,
	}
	recorder := NewAPICallStatsRecorder(boundary)
	ctx := ContextWithAPICallRecorder(context.Background(), recorder)

	RecordAPICall(ctx, APICallEvent{
		Boundary:  boundary,
		Operation: "DescribeRepositories",
		Result:    "success",
	})
	RecordAPICall(ctx, APICallEvent{
		Boundary:  boundary,
		Operation: "DescribeImages",
		Result:    "error",
		Throttled: true,
	})

	stats := recorder.Snapshot()
	if stats.APICallCount != 2 {
		t.Fatalf("APICallCount = %d, want 2", stats.APICallCount)
	}
	if stats.ThrottleCount != 1 {
		t.Fatalf("ThrottleCount = %d, want 1", stats.ThrottleCount)
	}
	if got := stats.OperationCounts["DescribeImages:error"]; got != 1 {
		t.Fatalf("DescribeImages error count = %d, want 1", got)
	}
}
