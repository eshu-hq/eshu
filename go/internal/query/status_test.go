package query

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusReportToMapIncludesBuildVersion(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf:   time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC),
		Health: status.HealthSummary{State: "healthy"},
		Queue:  status.QueueSnapshot{},
	}

	payload := statusReportToMap(report)
	if got, want := payload["version"], buildinfo.AppVersion(); got != want {
		t.Fatalf("statusReportToMap(report)[version] = %#v, want %#v", got, want)
	}
}
