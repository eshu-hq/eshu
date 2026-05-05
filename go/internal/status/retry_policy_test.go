package status_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestWithRetryPoliciesAttachesSnapshotMetadata(t *testing.T) {
	t.Parallel()

	reader := status.WithRetryPolicies(
		&fakeReader{
			snapshot: status.RawSnapshot{
				AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			},
		},
		status.RetryPolicySummary{
			Stage:       "projector",
			MaxAttempts: 5,
			RetryDelay:  45 * time.Second,
		},
		status.RetryPolicySummary{
			Stage:       "reducer",
			MaxAttempts: 3,
			RetryDelay:  30 * time.Second,
		},
	)

	raw, err := reader.ReadStatusSnapshot(context.Background(), time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v, want nil", err)
	}
	if got, want := len(raw.RetryPolicies), 2; got != want {
		t.Fatalf("RetryPolicies len = %d, want %d", got, want)
	}
	if got, want := raw.RetryPolicies[0].Stage, "projector"; got != want {
		t.Fatalf("RetryPolicies[0].Stage = %q, want %q", got, want)
	}
	if got, want := raw.RetryPolicies[0].MaxAttempts, 5; got != want {
		t.Fatalf("RetryPolicies[0].MaxAttempts = %d, want %d", got, want)
	}
}

func TestMergeRetryPoliciesOverridesByStage(t *testing.T) {
	t.Parallel()

	merged := status.MergeRetryPolicies(
		status.DefaultRetryPolicies(),
		status.RetryPolicySummary{
			Stage:       "projector",
			MaxAttempts: 7,
			RetryDelay:  90 * time.Second,
		},
	)

	if got, want := len(merged), 2; got != want {
		t.Fatalf("MergeRetryPolicies() len = %d, want %d", got, want)
	}
	if got, want := merged[0].Stage, "projector"; got != want {
		t.Fatalf("MergeRetryPolicies()[0].Stage = %q, want %q", got, want)
	}
	if got, want := merged[0].MaxAttempts, 7; got != want {
		t.Fatalf("MergeRetryPolicies()[0].MaxAttempts = %d, want %d", got, want)
	}
	if got, want := merged[1].Stage, "reducer"; got != want {
		t.Fatalf("MergeRetryPolicies()[1].Stage = %q, want %q", got, want)
	}
}

func TestRenderTextIncludesRetryPolicies(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			RetryPolicies: []status.RetryPolicySummary{
				{Stage: "projector", MaxAttempts: 4, RetryDelay: 42 * time.Second},
				{Stage: "reducer", MaxAttempts: 3, RetryDelay: 30 * time.Second},
			},
		},
		status.DefaultOptions(),
	)

	rendered := status.RenderText(report)
	for _, want := range []string{
		"Retry policies:",
		"projector max_attempts=4 retry_delay=42s",
		"reducer max_attempts=3 retry_delay=30s",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderText() missing %q in output:\n%s", want, rendered)
		}
	}
}

func TestRenderJSONIncludesRetryPolicies(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			RetryPolicies: []status.RetryPolicySummary{
				{Stage: "projector", MaxAttempts: 4, RetryDelay: 42 * time.Second},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	for _, want := range []string{
		`"retry_policies"`,
		`"stage": "projector"`,
		`"max_attempts": 4`,
		`"retry_delay": "42s"`,
		`"retry_delay_seconds": 42`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("RenderJSON() missing %q in payload:\n%s", want, payload)
		}
	}
}
