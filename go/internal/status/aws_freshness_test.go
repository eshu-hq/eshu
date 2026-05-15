package status_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestRenderStatusIncludesAWSFreshnessBacklog(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		AWSFreshness: status.AWSFreshnessSnapshot{
			StatusCounts: []status.NamedCount{
				{Name: "queued", Count: 2},
				{Name: "claimed", Count: 1},
				{Name: "handed_off", Count: 3},
				{Name: "failed", Count: 1},
			},
			OldestQueuedAge: 5 * time.Minute,
		},
	}, status.DefaultOptions())

	text := status.RenderText(report)
	for _, want := range []string{
		"AWS freshness:",
		"queued=2",
		"claimed=1",
		"handed_off=3",
		"failed=1",
		"oldest_queued=5m0s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q:\n%s", want, text)
		}
	}

	encoded, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["aws_freshness"] == nil {
		t.Fatalf("aws_freshness missing from JSON: %s", encoded)
	}
}
