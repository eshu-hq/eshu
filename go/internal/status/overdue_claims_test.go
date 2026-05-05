package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportClassifiesOverdueClaimsAsStalled(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:   3,
				OverdueClaims: 2,
			},
		},
		status.DefaultOptions(),
	)

	if report.Health.State != "stalled" {
		t.Fatalf("BuildReport().Health.State = %q, want %q", report.Health.State, "stalled")
	}
	if got := strings.Join(report.Health.Reasons, " "); !strings.Contains(got, "overdue claims") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want mention of overdue claims", report.Health.Reasons)
	}
	if got := report.Queue.OverdueClaims; got != 2 {
		t.Fatalf("BuildReport().Queue.OverdueClaims = %d, want 2", got)
	}
}
