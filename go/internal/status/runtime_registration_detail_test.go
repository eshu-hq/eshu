// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func TestCollectorRuntimeDetailAgreesWithCoordinatorRegistration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, registered := range []bool{true, false} {
		instances := []collector.InstanceSummary(nil)
		if registered {
			instances = []collector.InstanceSummary{{
				InstanceID:    "vulnerability-test",
				CollectorKind: "vulnerability_intelligence",
				Mode:          "continuous",
				Enabled:       true,
				ClaimsEnabled: true,
				UpdatedAt:     now,
			}}
		}
		report := status.BuildReport(status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: instances,
			},
			VulnerabilitySources: []collector.VulnerabilitySourceState{{
				CollectorInstanceID: "vulnerability-test",
				Source:              "osv",
				Ecosystem:           "npm",
				TerminalStatus:      "succeeded",
				UpdatedAt:           now,
			}},
		}, status.DefaultOptions())
		payload, err := status.RenderJSON(report)
		if err != nil {
			t.Fatalf("RenderJSON() error = %v", err)
		}
		var decoded struct {
			CollectorRuntimes []struct {
				CoordinatorRegistered bool   `json:"coordinator_registered"`
				Detail                string `json:"detail"`
			} `json:"collector_runtimes"`
		}
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if len(decoded.CollectorRuntimes) != 1 {
			t.Fatalf("collector runtimes = %d, want 1", len(decoded.CollectorRuntimes))
		}
		row := decoded.CollectorRuntimes[0]
		if row.CoordinatorRegistered != registered {
			t.Fatalf("coordinator_registered = %t, want %t", row.CoordinatorRegistered, registered)
		}
		if got := strings.Contains(row.Detail, "without workflow coordinator registration"); got == registered {
			t.Errorf("registered=%t detail=%q contradicts registration state", registered, row.Detail)
		}
	}
}

func BenchmarkCollectorRuntimeStatusesRegisteredSource(b *testing.B) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	evidence := collector.Evidence{
		Instances: []collector.InstanceSummary{{
			InstanceID:    "vulnerability-test",
			CollectorKind: "vulnerability_intelligence",
			Mode:          "continuous",
			Enabled:       true,
			ClaimsEnabled: true,
		}},
		VulnerabilitySources: []collector.VulnerabilitySourceState{{
			CollectorInstanceID: "vulnerability-test",
			Source:              "osv",
			Ecosystem:           "npm",
			TerminalStatus:      "succeeded",
			UpdatedAt:           now,
		}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := collector.RuntimeStatuses(evidence); len(got) != 1 {
			b.Fatalf("runtime statuses = %d, want 1", len(got))
		}
	}
}
